package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

// maxShareFileIDs caps one restore request, matching the other batch routes.
const maxShareFileIDs = 200

// registerShareRoutes serves the share browser: open a link, walk its
// folders, mint signed stream/download URLs, restore files, and keep
// favourites. The upstream pass-code token stays in the server-side session;
// the client only ever names a share id.
func registerShareRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/share", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		shareID, savedTitle, passCode, ok := resolveShareTarget(w, r, deps, id.UID)
		if !ok {
			return
		}
		// The share browser is a default-account surface: open, browse and
		// links all act as one identity, so the pass-code session they share
		// is keyed under that account's id.
		client, acct, ok := defaultAccountClient(w, r, deps)
		if !ok {
			return
		}
		info, err := client.GetShare(r.Context(), shareID, passCode)
		switch {
		case errors.Is(err, pikpak.ErrPassCodeRequired):
			writeError(w, http.StatusBadRequest, "pass_code_required", "该分享需要访问密码")
			return
		case errors.Is(err, pikpak.ErrPassCodeWrong):
			writeError(w, http.StatusBadRequest, "pass_code_wrong", "访问密码不正确")
			return
		case errors.Is(err, pikpak.ErrShareUnavailable):
			writeError(w, http.StatusNotFound, "share_unavailable", "分享已失效或已被取消")
			return
		case err != nil:
			writeUpstreamError(w, err)
			return
		}
		// The code the request carried — typed into the query or sealed in the
		// favourite — is remembered in plaintext, so a later restore can
		// reopen the share as another account.
		deps.ShareSessions.put(id.UID, acct.ID, shareID, info.PassCodeToken, passCode, time.Now())

		// 分享收藏夹 never counts a share; entering it does, off this request,
		// when the cached total is missing or older than a day.
		if fav, err := deps.Store.ShareFavoriteByShareID(r.Context(), id.UID, shareID); err == nil {
			startShareStats(deps, id.UID, fav)
		}

		title := info.Title
		if title == "" {
			title = savedTitle
		}
		if title == "" {
			title = shareID
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"share_id":        shareID,
			"title":           title,
			"owner":           info.Owner,
			"protected":       passCode != "",
			"items":           annotateShareFiles(r.Context(), deps, id.UID, shareID, info.Files),
			"next_page_token": info.NextPageToken,
		})
	})

	mux.HandleFunc("GET /api/share/files", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		shareID := strings.TrimSpace(r.URL.Query().Get("share_id"))
		client, acct, ok := defaultAccountClient(w, r, deps)
		if !ok {
			return
		}
		token, ok := shareToken(w, deps, id.UID, acct.ID, shareID)
		if !ok {
			return
		}
		page, err := client.ListShareFiles(r.Context(), shareID,
			strings.TrimSpace(r.URL.Query().Get("parent_id")), token,
			strings.TrimSpace(r.URL.Query().Get("page_token")))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":           annotateShareFiles(r.Context(), deps, id.UID, shareID, page.Items),
			"next_page_token": page.NextPageToken,
		})
	})

	mux.HandleFunc("GET /api/share/file", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		shareID := strings.TrimSpace(r.URL.Query().Get("share_id"))
		fileID := strings.TrimSpace(r.URL.Query().Get("file_id"))
		if fileID == "" {
			writeError(w, http.StatusBadRequest, "bad_ids", "缺少文件编号")
			return
		}
		client, acct, ok := defaultAccountClient(w, r, deps)
		if !ok {
			return
		}
		token, ok := shareToken(w, deps, id.UID, acct.ID, shareID)
		if !ok {
			return
		}
		link, err := client.GetShareFileLink(r.Context(), shareID, fileID, token)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		now := time.Now()
		t := playtoken.SignShare(deps.PlaySecret, id.UID, acct.ID, shareID, fileID, playTokenTTL, now)
		escShare, escFile := url.PathEscape(shareID), url.PathEscape(fileID)
		out := map[string]any{
			"file_id":      fileID,
			"name":         link.Name,
			"mime":         link.Mime,
			"size":         link.Size,
			"stream_url":   "/api/share/stream/" + escShare + "/" + escFile + "?t=" + t,
			"download_url": "/api/share/download/" + escShare + "/" + escFile + "?t=" + t,
		}
		// IINA plays the share's own link; the proxy URL cannot be handed to
		// an external player.
		if videoPlayer(r.Context(), deps, id.UID) == "iina" {
			out["direct_url"] = link.URL
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /api/share/restore", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ShareID string   `json:"share_id"`
			FileIDs []string `json:"file_ids"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		shareID := strings.TrimSpace(body.ShareID)
		if len(body.FileIDs) == 0 || len(body.FileIDs) > maxShareFileIDs {
			writeError(w, http.StatusBadRequest, "bad_ids", "一次需要选择 1 到 200 个文件")
			return
		}
		if shareID == "" {
			writeError(w, http.StatusBadRequest, "bad_share_link", "缺少分享编号")
			return
		}
		// The pass code lives in the default account's session — the one
		// opening the share seeded. Every account is reopened with it, because
		// each account needs its own pass-code token to restore.
		acct, err := deps.resolveAccount(r.Context(), id.UID, "")
		if err != nil {
			switch {
			case errors.Is(err, pikpak.ErrNotBound):
				writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
			default:
				writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
			}
			return
		}
		_, passCode, ok := deps.ShareSessions.get(id.UID, acct.ID, shareID, time.Now())
		if !ok {
			writeError(w, http.StatusBadRequest, "share_session_missing", "分享会话已过期，请重新打开分享")
			return
		}
		clients, ok := restoreClients(w, r, deps, acct)
		if !ok {
			return
		}
		// The restore lands on the first account that takes it, tried in
		// random order so the load spreads. A failure that is the account's
		// own (quota, rate limit, outage) rotates to the next; a refusal that
		// travels with the share — a wrong pass code above all — would fail on
		// every account the same way, so it stops the loop with the answer
		// already written.
		aw := &answeredWriter{ResponseWriter: w}
		type refusal struct{ name, reason string }
		var refused []refusal
		for _, nc := range clients {
			token, _, ok := openShareFor(aw, r, deps, id.UID, nc.Account, shareID, passCode)
			if !ok {
				if aw.wrote {
					return
				}
				refused = append(refused, refusal{name: accountLabel(nc.Account), reason: "打开分享失败"})
				continue
			}
			if _, err := nc.C.RestoreShare(r.Context(), shareID, token, body.FileIDs); err != nil {
				if !isAccountLevelErr(err) {
					writeUpstreamError(w, err)
					return
				}
				refused = append(refused, refusal{
					name:   accountLabel(nc.Account),
					reason: strings.TrimSpace(probe.Redact(err.Error())),
				})
				continue
			}
			resp := map[string]any{"ok": true}
			if nc.Account.ID != "" {
				resp["account_id"] = nc.Account.ID
				resp["account_name"] = accountLabel(nc.Account)
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		parts := make([]string, len(refused))
		for i, rf := range refused {
			parts[i] = rf.name + "：" + rf.reason
		}
		writeError(w, http.StatusTooManyRequests, "all_accounts_failed", strings.Join(parts, "；"))
	})

	registerShareFavoriteRoutes(mux, deps)
}

// resolveShareTarget reads the share from a link (or a favourite), returning
// the id and the pass code the request carries.
func resolveShareTarget(w http.ResponseWriter, r *http.Request, deps Deps, uid int) (shareID, title, passCode string, ok bool) {
	if raw := strings.TrimSpace(r.URL.Query().Get("favorite_id")); raw != "" {
		favID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
			return "", "", "", false
		}
		fav, err := deps.Store.GetShareFavorite(r.Context(), uid, favID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "favorite_not_found", "收藏不存在")
			return "", "", "", false
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "读取收藏失败")
			return "", "", "", false
		}
		return fav.ShareID, fav.Title, fav.PassCode, true
	}

	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("share_id"))
	}
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_share_link", "请提供分享链接")
		return "", "", "", false
	}
	shareID, fromLink, err := pikpak.ParseShareLink(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_share_link", "这不是 PikPak 分享链接")
		return "", "", "", false
	}
	passCode = strings.TrimSpace(r.URL.Query().Get("pass_code"))
	if passCode == "" {
		passCode = fromLink
	}
	return shareID, "", passCode, true
}

// shareToken returns the cached pass-code token of a share opened under one
// account. Sessions are per account, so every reader must name the account
// the open wrote.
func shareToken(w http.ResponseWriter, deps Deps, uid int, accountID, shareID string) (string, bool) {
	if shareID == "" {
		writeError(w, http.StatusBadRequest, "bad_share_link", "缺少分享编号")
		return "", false
	}
	token, _, ok := deps.ShareSessions.get(uid, accountID, shareID, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "share_session_missing", "分享会话已过期，请重新打开分享")
		return "", false
	}
	return token, true
}

// answeredWriter records whether a rotation step already answered the client.
// openShareFor answers share-level refusals itself (no account can do better)
// and leaves account-level failures unwritten, so this is how the restore
// loop tells one from the other and decides between stopping and rotating.
type answeredWriter struct {
	http.ResponseWriter
	wrote bool
}

func (a *answeredWriter) WriteHeader(code int) {
	a.wrote = true
	a.ResponseWriter.WriteHeader(code)
}

func (a *answeredWriter) Write(p []byte) (int, error) {
	a.wrote = true
	return a.ResponseWriter.Write(p)
}

// restoreClients builds the restore rotation: every active account in random
// order, with its session loaded. A user bound before accounts existed owns
// exactly the legacy placeholder identity passed as fallback. Unloadable
// sessions are skipped; with nothing usable the structured refusal is written
// and ok is false.
func restoreClients(w http.ResponseWriter, r *http.Request, deps Deps, fallback store.PikPakAccount) ([]namedClient, bool) {
	id, _ := fnos.IdentityFrom(r.Context())
	accounts, err := deps.Store.ListAccounts(r.Context(), id.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "无法读取账号")
		return nil, false
	}
	rotation := shuffledActives(accounts)
	if len(accounts) == 0 {
		rotation = []store.PikPakAccount{fallback}
	}
	var out []namedClient
	for _, acct := range rotation {
		client, err := deps.Sessions.ClientForAccount(r.Context(), id.UID, acct.ID)
		if err != nil {
			continue
		}
		out = append(out, namedClient{Account: acct, C: client})
	}
	if len(out) == 0 {
		writeError(w, http.StatusUnauthorized, "no_active_account", "没有可用的 PikPak 账号，请重新授权或绑定")
		return nil, false
	}
	return out, true
}

// openShareFor opens a share as one account and seeds that account's
// pass-code session with the code used. Share-level refusals — wrong code,
// gone share, anything that would repeat on every account — are answered
// here; account-level failures return unwritten so a rotation loop can try
// the next account. ok false with nothing written means exactly that.
func openShareFor(w http.ResponseWriter, r *http.Request, deps Deps, uid int, acct store.PikPakAccount, shareID, passCode string) (string, *pikpak.ShareInfo, bool) {
	client, err := deps.Sessions.ClientForAccount(r.Context(), uid, acct.ID)
	if err != nil {
		return "", nil, false
	}
	info, err := client.GetShare(r.Context(), shareID, passCode)
	if err != nil {
		switch {
		case errors.Is(err, pikpak.ErrPassCodeRequired):
			writeError(w, http.StatusBadRequest, "pass_code_required", "该分享需要访问密码")
		case errors.Is(err, pikpak.ErrPassCodeWrong):
			writeError(w, http.StatusBadRequest, "pass_code_wrong", "访问密码不正确")
		case errors.Is(err, pikpak.ErrShareUnavailable):
			writeError(w, http.StatusNotFound, "share_unavailable", "分享已失效或已被取消")
		case !isAccountLevelErr(err):
			writeUpstreamError(w, err)
		}
		return "", nil, false
	}
	deps.ShareSessions.put(uid, acct.ID, shareID, info.PassCodeToken, passCode, time.Now())
	return info.PassCodeToken, &info, true
}

// migrateShareFavorites rewrites rows saved before links were parsed on add:
// a favorite stored under a pasted link becomes its share id, with the code
// re-sealed under the new key. The list is the only writer, so a read of
// 分享收藏夹 is where historical data is tidied.
func migrateShareFavorites(ctx context.Context, deps Deps, uid int, items []store.ShareFavorite) []store.ShareFavorite {
	out := make([]store.ShareFavorite, 0, len(items))
	for _, fav := range items {
		shareID, fromLink, err := pikpak.ParseShareLink(fav.ShareID)
		if err != nil || shareID == fav.ShareID {
			out = append(out, fav)
			continue
		}
		code := fromLink
		if full, err := deps.Store.GetShareFavorite(ctx, uid, fav.ID); err == nil && full.PassCode != "" {
			code = full.PassCode
		}
		title := fav.Title
		if title == "" || title == fav.ShareID {
			title = shareID
		}
		updated, err := deps.Store.MoveShareFavoriteShare(ctx, uid, fav.ID, shareID, title, code)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue // merged into a row that already had this share
			}
			out = append(out, fav)
			continue
		}
		updated.PassCode = ""
		out = append(out, updated)
	}
	return out
}

func registerShareFavoriteRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/share-favorites", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		// A read path: whatever the cache holds, with no PikPak round trip.
		items, err := deps.Store.ListShareFavorites(r.Context(), id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "读取收藏失败")
			return
		}
		items = migrateShareFavorites(r.Context(), deps, id.UID, items)
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	registerShareFavoriteAvatar(mux, deps)

	mux.HandleFunc("POST /api/share-favorites", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL      string `json:"url"`
			ShareID  string `json:"share_id"`
			Title    string `json:"title"`
			PassCode string `json:"pass_code"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		raw := strings.TrimSpace(body.URL)
		if raw == "" {
			raw = strings.TrimSpace(body.ShareID)
		}
		shareID, fromLink, err := pikpak.ParseShareLink(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_share_link", "这不是 PikPak 分享链接")
			return
		}
		passCode := strings.TrimSpace(body.PassCode)
		if passCode == "" {
			passCode = fromLink
		}
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = shareID
		}
		fav, err := deps.Store.AddShareFavorite(r.Context(), id.UID, shareID, title, passCode)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "保存收藏失败")
			return
		}
		// Best effort: remember who shared it while the share is at hand. The
		// totals wait for the first visit, where the share is walked in the
		// background.
		if client, err := deps.Sessions.ClientFor(r.Context(), id.UID); err == nil {
			metaCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			fav = refreshShareFavoriteStats(metaCtx, deps, id.UID, fav, client, false)
			cancel()
		}
		writeJSON(w, http.StatusCreated, fav)
	})

	mux.HandleFunc("PATCH /api/share-favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		favID, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		var body struct {
			Title string `json:"title"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		title := strings.TrimSpace(body.Title)
		if utf8.RuneCountInString(title) > 100 {
			writeError(w, http.StatusBadRequest, "bad_title", "显示名太长")
			return
		}
		fav, err := deps.Store.RenameShareFavorite(r.Context(), id.UID, favID, title)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "favorite_not_found", "收藏不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "重命名失败")
			return
		}
		writeJSON(w, http.StatusOK, fav)
	})

	mux.HandleFunc("DELETE /api/share-favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		favID, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := deps.Store.DeleteShareFavorite(r.Context(), id.UID, favID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "favorite_not_found", "收藏不存在")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal", "删除收藏失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// accountLabel names an account in user-facing messages: the display name
// when the row carries one, else the raw id — a dangling "账号：" would
// explain nothing.
func accountLabel(acct store.PikPakAccount) string {
	if acct.DisplayName != "" {
		return acct.DisplayName
	}
	return acct.ID
}

// annotateShareFiles adds the two local badges the share browser shows on a
// video: restored when any account's Pack From Shared already holds a copy
// with its name and size (the copy 完整播放 would reuse), favorited when it is
// in 视频收藏夹. The browser shows the star when both are set. Drives that
// cannot be read simply carry no badges — the listing itself still shows.
func annotateShareFiles(ctx context.Context, deps Deps, uid int, shareID string, files []pikpak.File) []map[string]any {
	// A saved video is found by its share file id. PikPak can reissue that id
	// for the same entry, so name and size — the pair a restored copy is
	// already matched by — stand in when the id no longer resolves.
	type videoKey struct {
		name string
		size int64
	}
	favorited := map[string]bool{}
	favoritedByName := map[videoKey]bool{}
	if deps.Store != nil {
		if favs, err := deps.Store.VideoFavoritesInShare(ctx, uid, shareID); err == nil {
			for _, fav := range favs {
				favorited[fav.FileID] = true
				favoritedByName[videoKey{fav.Name, fav.Size}] = true
			}
		}
	}
	copies := packCopies(ctx, deps, uid)
	restored := map[string]bool{}
	for _, f := range files {
		if f.Kind != "drive#file" {
			continue
		}
		if _, _, ok := findCopyAcross(copies, "", f.Name, f.Name, f.Size); ok {
			restored[f.ID] = true
		}
	}
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		raw, _ := json.Marshal(f)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		isFavorite := favorited[f.ID]
		if !isFavorite && f.Kind != "drive#folder" {
			isFavorite = favoritedByName[videoKey{f.Name, f.Size}]
		}
		item["restored"] = restored[f.ID]
		item["favorited"] = isFavorite
		out = append(out, item)
	}
	return out
}
