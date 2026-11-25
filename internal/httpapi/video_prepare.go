package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

// The folders 完整播放 works with. A restore lands in Pack From Shared, which
// is reused on every later play; 临时 is an older folder that is purged first
// when room has to be made, never restored into.
const (
	videoFolderName    = "我的视频"
	tempFolderName     = "临时"
	packFromSharedName = "Pack From Shared"

	videoTaskTimeout  = 2 * time.Minute
	videoTaskInterval = 500 * time.Millisecond
)

// The errors a route or a job reports; the messages are the user-facing copy.
var (
	errSpaceInsufficient = errors.New("空间不足：清理「我的视频/临时」和「Pack From Shared」后仍无法容纳该视频")
	errNotBound          = errors.New("请先绑定 PikPak 账号")
	errSessionGone       = errors.New("分享会话已过期，请重新打开分享")
	errFavoriteMissing   = errors.New("收藏不存在")
	errSessionLoad       = errors.New("无法加载 PikPak 会话")
	errPassRequired      = errors.New("该分享需要访问密码")
	errPassWrong         = errors.New("分享访问密码不正确")
	errShareUnavailable  = errors.New("分享已失效或已被取消")
	errVideoNoLink       = errors.New("该文件没有可播放的链接")
	errReauth            = errors.New("PikPak 授权已失效，请重新绑定账号")
	errUpstream          = errors.New("PikPak 请求失败，请稍后重试")
	errAllAccountsFailed = errors.New("所有账号都无法完成转存")
)

// videoPrepareRequest names the share video to prepare. A favorite carries a
// sealed pass code, so it reopens without asking; a share tab relies on its
// server-side session.
type videoPrepareRequest struct {
	ShareID string
	FileID  string
	// FavoriteID is positive when the video comes from 视频收藏夹.
	FavoriteID int64
}

// videoSource is the share file a prepare or preview plays. passCode is the
// code the share was opened with, so the account rotation can reopen the
// share under other accounts' identities.
type videoSource struct {
	shareID  string
	fileID   string
	token    string
	passCode string
	owner    string
	title    string
	favorite *store.VideoFavorite
}

// prepareVideo restores one share video into Pack From Shared, making room
// first, and returns a signed URL for that copy. A copy an earlier 完整播放
// left in ANY account's drive is reused: the video is never restored twice.
//
// 转存换号（spec §10）：先在默认账号上解析来源与 share link；任何账号的
// Pack From Shared 已有匹配副本就直接用副本账号播放并回写收藏；未命中则
// 按随机顺序逐账号：makeRoom → openShareAs → RestoreShare → waitTask。
// restore 任务落定前的失败换下一个账号，落定后的失败只报告——副本已经存
// 在，别的账号重试只会造成重复。耗尽时报 errAllAccountsFailed。
func prepareVideo(ctx context.Context, deps Deps, uid int, req videoPrepareRequest) (videoPrepareResult, error) {
	client, err := deps.Sessions.ClientForAccount(ctx, uid, "")
	if errors.Is(err, pikpak.ErrNotBound) {
		return videoPrepareResult{}, errNotBound
	}
	if err != nil {
		return videoPrepareResult{}, errSessionLoad
	}

	// 来源与 share link 仍走默认账号：分享页和收藏夹都在默认账号下打开，
	// pass-code 会话也以它为键。
	acctID := defaultAccountID(ctx, deps, uid)
	source, err := resolveVideoSource(ctx, deps, uid, req, client, acctID)
	if err != nil {
		return videoPrepareResult{}, err
	}
	link, err := client.GetShareFileLink(ctx, source.shareID, source.fileID, source.token)
	if err != nil {
		return videoPrepareResult{}, videoUpstreamMessage(err)
	}
	// 转存后的文件名 uses the local rename when there is one.
	targetName := link.Name
	var restoredID string
	if source.favorite != nil {
		if name := strings.TrimSpace(source.favorite.DisplayName); name != "" {
			targetName = name
		}
		restoredID = source.favorite.RestoredFileID
	}
	if source.favorite != nil {
		// Refresh the saved metadata so the favourites list shows the real
		// name, size and owner; the display name and pass code stay as saved.
		_, _ = deps.Store.AddVideoFavorite(ctx, uid, store.VideoFavorite{
			ShareID: source.shareID, FileID: source.fileID,
			Name: link.Name, Size: link.Size,
			Owner: source.owner, ShareTitle: source.title,
		})
	}

	// 跨账号复用：任何账号的既有副本（先按记住的 id，再按名字+大小）都能
	// 短路整个转存。
	if found, foundAcct, ok := findCopyAcross(packCopies(ctx, deps, uid), restoredID, targetName, link.Name, link.Size); ok {
		copyClient, err := deps.Sessions.ClientForAccount(ctx, uid, foundAcct)
		if err != nil {
			return videoPrepareResult{}, errSessionLoad
		}
		return reuseRestoredCopy(ctx, deps, copyClient, uid, foundAcct, source, found, targetName)
	}

	accounts, err := deps.Store.ListAccounts(ctx, uid)
	if err != nil {
		return videoPrepareResult{}, errSessionLoad
	}
	rotation := shuffledActives(accounts)
	if len(accounts) == 0 {
		// A user bound before accounts existed owns exactly the legacy
		// placeholder identity; it is the only account to try.
		if fallback, err := deps.resolveAccount(ctx, uid, ""); err == nil {
			rotation = []store.PikPakAccount{fallback}
		}
	}
	var failures []string
	for _, acct := range rotation {
		res, reason, err := restoreOnAccount(ctx, deps, uid, acct, source, link, targetName)
		if err != nil {
			// The failure would repeat on every account (share-level
			// refusal) or happened after the restore task settled and the
			// copy exists — either way, report instead of rotating.
			return videoPrepareResult{}, err
		}
		if reason == "" {
			return res, nil
		}
		failures = append(failures, accountLabel(acct)+"："+reason)
	}
	return videoPrepareResult{}, fmt.Errorf("%w：%s", errAllAccountsFailed, strings.Join(failures, "；"))
}

// restoreOnAccount runs one account's whole restore pipeline: snapshot the
// folders, make room, open the share as this account, restore, wait. It
// answers one of three ways:
//
//	(result, "", nil)  the copy exists on this account — done
//	({}, reason, nil)  this account failed; try the next one
//	({}, "", err)      stop and report err: the refusal travels with the
//	                   share, or the restore task already settled so another
//	                   account retrying would only duplicate the copy
func restoreOnAccount(ctx context.Context, deps Deps, uid int, acct store.PikPakAccount, source videoSource, link pikpak.ShareFileLink, targetName string) (videoPrepareResult, string, error) {
	client, err := deps.Sessions.ClientForAccount(ctx, uid, acct.ID)
	if err != nil {
		return videoPrepareResult{}, "无法加载会话", nil
	}

	// PikPak drops a restored share into the root folder "Pack From Shared",
	// creating it on the first restore; older accounts see the file in the
	// root itself. Both are snapshotted before the restore so the freshly
	// restored file can be told apart from same-named files already there.
	beforeRoot, err := client.ListAllFiles(ctx, "")
	if err != nil {
		return videoPrepareResult{}, rotateReason(err), nil
	}
	var beforePack []pikpak.File
	if pack, ok := findFolder(beforeRoot, packFromSharedName); ok {
		if beforePack, err = client.ListAllFiles(ctx, pack.ID); err != nil {
			return videoPrepareResult{}, rotateReason(err), nil
		}
	}

	if err := makeRoom(ctx, client, link.Size); err != nil {
		return videoPrepareResult{}, rotateReason(err), nil
	}
	knownRoot := fileIDs(beforeRoot)
	knownPack := fileIDs(beforePack)
	token, _, err := openShareAs(ctx, deps, uid, acct, client, source.shareID, source.passCode)
	if err != nil {
		switch {
		case errors.Is(err, pikpak.ErrPassCodeRequired):
			return videoPrepareResult{}, "", errPassRequired
		case errors.Is(err, pikpak.ErrPassCodeWrong):
			return videoPrepareResult{}, "", errPassWrong
		case errors.Is(err, pikpak.ErrShareUnavailable):
			return videoPrepareResult{}, "", errShareUnavailable
		case !isAccountLevelErr(err):
			return videoPrepareResult{}, "", videoUpstreamMessage(err)
		}
		return videoPrepareResult{}, rotateReason(err), nil
	}
	restoreTask, err := client.RestoreShare(ctx, source.shareID, token, []string{source.fileID})
	if err != nil {
		if !isAccountLevelErr(err) {
			return videoPrepareResult{}, "", videoUpstreamMessage(err)
		}
		return videoPrepareResult{}, rotateReason(err), nil
	}
	if err := waitTask(ctx, client, restoreTask); err != nil {
		return videoPrepareResult{}, rotateReason(err), nil
	}

	// 从这里开始 restore 任务已经落定：副本已存在，之后的失败只报告，不
	// 再换号重试——别的账号再转存只会造成重复。
	afterRoot, err := client.ListAllFiles(ctx, "")
	if err != nil {
		return videoPrepareResult{}, "", videoUpstreamMessage(err)
	}
	var afterPack []pikpak.File
	if pack, ok := findFolder(afterRoot, packFromSharedName); ok {
		afterPack, err = client.ListAllFiles(ctx, pack.ID)
		if err != nil {
			return videoPrepareResult{}, "", videoUpstreamMessage(err)
		}
	}
	restored, ok := findRestoredFile(afterPack, knownPack, link.Name)
	if !ok {
		restored, ok = findRestoredFile(afterRoot, knownRoot, link.Name)
	}
	if !ok {
		return videoPrepareResult{}, "", errors.New("转存已完成，但没有找到该视频")
	}
	if restored.Name != targetName {
		if err := client.Rename(ctx, restored.ID, targetName); err != nil {
			return videoPrepareResult{}, "", videoUpstreamMessage(err)
		}
		restored.Name = targetName
	}
	recordRestoredCopy(ctx, deps, uid, source, restored.ID, acct.ID)
	res, err := finishedVideoResult(ctx, deps, client, uid, acct.ID, restored)
	return res, "", err
}

// rotateReason renders one account's failure for the all_accounts_failed
// envelope: redacted upstream text, exactly what the share-restore loop
// reports per account.
func rotateReason(err error) string {
	return strings.TrimSpace(probe.Redact(err.Error()))
}

// reuseRestoredCopy plays a copy an earlier 完整播放 left in Pack From Shared
// (on any account), renaming it to the local name when that changed since.
func reuseRestoredCopy(ctx context.Context, deps Deps, client *pikpak.Client, uid int, accountID string, source videoSource, file pikpak.File, targetName string) (videoPrepareResult, error) {
	if file.Name != targetName {
		if err := client.Rename(ctx, file.ID, targetName); err != nil {
			return videoPrepareResult{}, videoUpstreamMessage(err)
		}
		file.Name = targetName
	}
	recordRestoredCopy(ctx, deps, uid, source, file.ID, accountID)
	return finishedVideoResult(ctx, deps, client, uid, accountID, file)
}

// recordRestoredCopy remembers which account's drive file backs the
// favourite; the list then marks it ✅ and the next play skips the restore.
func recordRestoredCopy(ctx context.Context, deps Deps, uid int, source videoSource, fileID, accountID string) {
	if source.favorite == nil {
		return
	}
	_ = deps.Store.SetVideoFavoriteRestoredID(ctx, uid, source.favorite.ID, fileID, accountID)
}

// finishedVideoResult signs the proxy URL under the copy's own account and,
// when 设置 says IINA, adds the file's own CDN link so the external player
// never goes through the proxy.
func finishedVideoResult(ctx context.Context, deps Deps, client *pikpak.Client, uid int, accountID string, file pikpak.File) (videoPrepareResult, error) {
	out, err := signVideoResult(deps, uid, accountID, file)
	if err != nil {
		return videoPrepareResult{}, err
	}
	if videoPlayer(ctx, deps, uid) == "iina" {
		link, err := client.GetDownloadLink(ctx, file.ID)
		if err != nil {
			return videoPrepareResult{}, videoUpstreamMessage(err)
		}
		if link.URL == "" {
			return videoPrepareResult{}, errVideoNoLink
		}
		out.DirectURL = link.URL
	}
	return out, nil
}

// previewShareVideo signs the share's own stream URL: nothing is restored, so
// the video plays straight from the share. It acts as the default account,
// the same one the share browser opens with.
func previewShareVideo(ctx context.Context, deps Deps, uid int, req videoPrepareRequest) (videoPreviewResult, error) {
	client, err := deps.Sessions.ClientForAccount(ctx, uid, "")
	if errors.Is(err, pikpak.ErrNotBound) {
		return videoPreviewResult{}, errNotBound
	}
	if err != nil {
		return videoPreviewResult{}, errSessionLoad
	}
	acctID := defaultAccountID(ctx, deps, uid)
	source, err := resolveVideoSource(ctx, deps, uid, req, client, acctID)
	if err != nil {
		return videoPreviewResult{}, err
	}
	link, err := client.GetShareFileLink(ctx, source.shareID, source.fileID, source.token)
	if err != nil {
		return videoPreviewResult{}, videoUpstreamMessage(err)
	}
	token, err := playtoken.SignShare(deps.PlaySecret, uid, acctID, source.shareID, source.fileID, playTokenTTL, time.Now())
	if err != nil {
		return videoPreviewResult{}, err
	}
	out := videoPreviewResult{
		StreamURL: "/api/share/stream/" + url.PathEscape(source.shareID) + "/" + url.PathEscape(source.fileID) + "?t=" + token,
	}
	if videoPlayer(ctx, deps, uid) == "iina" {
		out.DirectURL = link.URL
	}
	return out, nil
}

// videoPreviewResult is what 预览 answers: the proxy URL the built-in player
// uses, and, for IINA, the share's own CDN link.
type videoPreviewResult struct {
	StreamURL string `json:"stream_url"`
	DirectURL string `json:"direct_url,omitempty"`
}

// videoPlayer returns "iina" when the user chose IINA in 设置, else "builtin".
func videoPlayer(ctx context.Context, deps Deps, uid int) string {
	if deps.Store == nil {
		return "builtin"
	}
	if deps.Store.EffectiveSetting(ctx, uid, "player") == "iina" {
		return "iina"
	}
	return "builtin"
}

// resolveVideoSource locates the share file a prepare or preview asks for.
// acctID is the account the caller acts as — the key the share's pass-code
// session is stored under.
func resolveVideoSource(ctx context.Context, deps Deps, uid int, req videoPrepareRequest, client *pikpak.Client, acctID string) (videoSource, error) {
	if req.FavoriteID > 0 {
		fav, err := deps.Store.GetVideoFavorite(ctx, uid, req.FavoriteID)
		if errors.Is(err, store.ErrNotFound) {
			return videoSource{}, errFavoriteMissing
		}
		if err != nil {
			return videoSource{}, errors.New("读取收藏失败")
		}
		info, err := client.GetShare(ctx, fav.ShareID, fav.PassCode)
		if err != nil {
			return videoSource{}, videoUpstreamMessage(err)
		}
		deps.ShareSessions.put(uid, acctID, fav.ShareID, info.PassCodeToken, fav.PassCode, time.Now())
		return videoSource{
			shareID: fav.ShareID, fileID: fav.FileID,
			token: info.PassCodeToken, passCode: fav.PassCode,
			owner: info.Owner, title: info.Title,
			favorite: &fav,
		}, nil
	}
	token, passCode, ok := deps.ShareSessions.get(uid, acctID, req.ShareID, time.Now())
	if !ok {
		return videoSource{}, errSessionGone
	}
	return videoSource{
		shareID: req.ShareID, fileID: req.FileID,
		token: token, passCode: passCode,
	}, nil
}

// videoUpstreamMessage turns the upstream errors a person can act on into
// Chinese copy; anything else stays generic rather than leaking a URL.
func videoUpstreamMessage(err error) error {
	switch {
	case errors.Is(err, pikpak.ErrPassCodeRequired):
		return errPassRequired
	case errors.Is(err, pikpak.ErrPassCodeWrong):
		return errPassWrong
	case errors.Is(err, pikpak.ErrShareUnavailable):
		return errShareUnavailable
	case errors.Is(err, pikpak.ErrNoDownloadLink):
		return errVideoNoLink
	case errors.Is(err, pikpak.ErrReauthRequired):
		return errReauth
	default:
		return errUpstream
	}
}

// tempFolderID returns the id of 我的视频/临时, or "" when the folders do not
// exist yet (nothing has been prepared before).
func tempFolderID(ctx context.Context, client *pikpak.Client) (string, error) {
	root, err := client.ListAllFiles(ctx, "")
	if err != nil {
		return "", err
	}
	videoFolder, ok := findFolder(root, videoFolderName)
	if !ok {
		return "", nil
	}
	children, err := client.ListAllFiles(ctx, videoFolder.ID)
	if err != nil {
		return "", err
	}
	if temp, ok := findFolder(children, tempFolderName); ok {
		return temp.ID, nil
	}
	return "", nil
}

func findFolder(files []pikpak.File, name string) (pikpak.File, bool) {
	for _, f := range files {
		if f.Kind == "drive#folder" && f.Name == name {
			return f, true
		}
	}
	return pikpak.File{}, false
}

func findSameVideo(files []pikpak.File, name string, size int64) (pikpak.File, bool) {
	for _, f := range files {
		if f.Kind != "drive#file" || f.Name != name {
			continue
		}
		if size == 0 || f.Size == size {
			return f, true
		}
	}
	return pikpak.File{}, false
}

// makeRoom frees enough quota for need bytes. It purges the oldest files in
// 我的视频/临时 first, then the oldest files in the root Pack From Shared
// folder, re-reading the quota after each settled delete because PikPak's
// usage figure lags the task. It returns errSpaceInsufficient when both
// folders are exhausted.
func makeRoom(ctx context.Context, client *pikpak.Client, need int64) error {
	if need <= 0 {
		return nil
	}
	free, err := freeSpace(ctx, client)
	if err != nil {
		return videoUpstreamMessage(err)
	}
	if free >= need {
		return nil
	}

	if tempID, err := tempFolderID(ctx, client); err != nil {
		return videoUpstreamMessage(err)
	} else if tempID != "" {
		if err := purgeOldest(ctx, client, tempID, need); err != nil {
			return err
		}
	}
	if free, err = freeSpace(ctx, client); err != nil {
		return videoUpstreamMessage(err)
	}
	if free >= need {
		return nil
	}

	root, err := client.ListAllFiles(ctx, "")
	if err != nil {
		return videoUpstreamMessage(err)
	}
	if pack, ok := findFolder(root, packFromSharedName); ok {
		if err := purgeOldest(ctx, client, pack.ID, need); err != nil {
			return err
		}
	}
	if free, err = freeSpace(ctx, client); err != nil {
		return videoUpstreamMessage(err)
	}
	if free < need {
		return errSpaceInsufficient
	}
	return nil
}

func freeSpace(ctx context.Context, client *pikpak.Client) (int64, error) {
	about, err := client.About(ctx)
	if err != nil {
		return 0, err
	}
	return about.QuotaLimit - about.QuotaUsage, nil
}

// purgeOldest deletes files from parentID oldest first until the quota fits.
// Folders are left alone: they are cheap and may be someone's structure.
func purgeOldest(ctx context.Context, client *pikpak.Client, parentID string, need int64) error {
	items, err := client.ListAllFiles(ctx, parentID)
	if err != nil {
		return videoUpstreamMessage(err)
	}
	files := make([]pikpak.File, 0, len(items))
	for _, f := range items {
		if f.Kind == "drive#file" {
			files = append(files, f)
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		return modifiedSortKey(files[i]) < modifiedSortKey(files[j])
	})
	for _, f := range files {
		free, err := freeSpace(ctx, client)
		if err != nil {
			return videoUpstreamMessage(err)
		}
		if free >= need {
			return nil
		}
		task, err := client.BatchDelete(ctx, []string{f.ID})
		if err != nil {
			return videoUpstreamMessage(err)
		}
		if err := waitTask(ctx, client, task); err != nil {
			return err
		}
	}
	return nil
}

// modifiedSortKey normalises PikPak's timestamps for oldest-first sorting.
// An unparsable value sorts first, which only ever deletes something older
// than it can prove.
func modifiedSortKey(f pikpak.File) string {
	t, err := time.Parse(time.RFC3339Nano, f.ModifiedTime)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// fileIDs is the set of ids in one folder listing.
func fileIDs(files []pikpak.File) map[string]bool {
	out := make(map[string]bool, len(files))
	for _, f := range files {
		out[f.ID] = true
	}
	return out
}

// findRestoredFile picks the file the restore just dropped into a folder.
// Comparing against the pre-restore snapshot matters: a file of the same name
// may already be there, and moving that one would play the wrong video. A nil
// known set means the folder itself is new.
func findRestoredFile(after []pikpak.File, known map[string]bool, name string) (pikpak.File, bool) {
	var first pikpak.File
	for _, f := range after {
		if known[f.ID] || f.Kind != "drive#file" {
			continue
		}
		if f.Name == name {
			return f, true
		}
		if first.ID == "" {
			first = f
		}
	}
	return first, first.ID != ""
}

func waitTask(ctx context.Context, client *pikpak.Client, taskID string) error {
	if taskID == "" {
		return nil
	}
	_, ok, err := client.WaitTaskSettled(ctx, taskID, videoTaskTimeout, videoTaskInterval)
	if err != nil {
		return videoUpstreamMessage(err)
	}
	if !ok {
		return errors.New("PikPak 任务超时，请稍后重试")
	}
	return nil
}

// signVideoResult signs the proxy URL under the copy's own account: the
// stream endpoint then spends exactly that account's session.
func signVideoResult(deps Deps, uid int, accountID string, file pikpak.File) (videoPrepareResult, error) {
	token, err := playtoken.Sign(deps.PlaySecret, uid, accountID, file.ID, playTokenTTL, time.Now())
	if err != nil {
		return videoPrepareResult{}, err
	}
	return videoPrepareResult{
		FileID:    file.ID,
		AccountID: accountID,
		Name:      file.Name,
		Size:      file.Size,
		StreamURL: "/api/stream/" + url.PathEscape(file.ID) + "?t=" + token,
	}, nil
}

// findFileByID returns the file with the given id from one folder listing.
func findFileByID(files []pikpak.File, id string) (pikpak.File, bool) {
	for _, f := range files {
		if f.ID == id {
			return f, true
		}
	}
	return pikpak.File{}, false
}
