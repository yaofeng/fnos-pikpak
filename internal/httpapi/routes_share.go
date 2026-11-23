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
		client, ok := boundClient(w, r, deps)
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
		deps.ShareSessions.put(id.UID, shareID, info.PassCodeToken, time.Now())

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
			"items":           annotateShareFiles(r.Context(), deps, client, id.UID, shareID, info.Files),
			"next_page_token": info.NextPageToken,
		})
	})

	mux.HandleFunc("GET /api/share/files", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		shareID := strings.TrimSpace(r.URL.Query().Get("share_id"))
		token, ok := shareToken(w, deps, id.UID, shareID)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
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
			"items":           annotateShareFiles(r.Context(), deps, client, id.UID, shareID, page.Items),
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
		token, ok := shareToken(w, deps, id.UID, shareID)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		link, err := client.GetShareFileLink(r.Context(), shareID, fileID, token)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		now := time.Now()
		t := playtoken.SignShare(deps.PlaySecret, id.UID, shareID, fileID, playTokenTTL, now)
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
		token, ok := shareToken(w, deps, id.UID, shareID)
		if !ok {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if _, err := client.RestoreShare(r.Context(), shareID, token, body.FileIDs); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

// shareToken returns the cached pass-code token for an opened share.
func shareToken(w http.ResponseWriter, deps Deps, uid int, shareID string) (string, bool) {
	if shareID == "" {
		writeError(w, http.StatusBadRequest, "bad_share_link", "缺少分享编号")
		return "", false
	}
	token, ok := deps.ShareSessions.get(uid, shareID, time.Now())
	if !ok {
		writeError(w, http.StatusBadRequest, "share_session_missing", "分享会话已过期，请重新打开分享")
		return "", false
	}
	return token, true
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
		items, pending, err := listShareFavoritesWithStats(r.Context(), deps, id.UID,
			r.URL.Query().Get("refresh") == "1")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "读取收藏失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "stats_pending": pending})
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
		// Best effort: remember what PikPak reported while the share is at
		// hand. The totals are counted when the list asks for them.
		if client, err := deps.Sessions.ClientFor(r.Context(), id.UID); err == nil {
			metaCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			fav = refreshShareFavoriteStats(metaCtx, deps, id.UID, fav, client, false)
			cancel()
		}
		writeJSON(w, http.StatusCreated, fav)
	})

	mux.HandleFunc("PATCH /api/share-favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		favID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
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
		favID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
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

// annotateShareFiles adds the two local badges the share browser shows on a
// video: restored when Pack From Shared already holds a copy with its name and
// size (the copy 完整播放 would reuse), favorited when it is in 视频收藏夹. The
// restore wins on the tile. A drive that cannot be read simply carries no
// badges — the listing itself still shows.
func annotateShareFiles(ctx context.Context, deps Deps, client *pikpak.Client, uid int, shareID string, files []pikpak.File) []map[string]any {
	favorited := map[string]bool{}
	if deps.Store != nil {
		if ids, err := deps.Store.VideoFavoriteFileIDs(ctx, uid, shareID); err == nil {
			favorited = ids
		}
	}
	restored := map[string]bool{}
	if root, err := client.ListAllFiles(ctx, ""); err == nil {
		var pack []pikpak.File
		if folder, ok := findFolder(root, packFromSharedName); ok {
			if items, err := client.ListAllFiles(ctx, folder.ID); err == nil {
				pack = items
			}
		}
		for _, f := range files {
			if f.Kind != "drive#file" {
				continue
			}
			if _, ok := findSameVideo(pack, f.Name, f.Size); ok {
				restored[f.ID] = true
			}
		}
	}
	out := make([]map[string]any, 0, len(files))
	for _, f := range files {
		raw, _ := json.Marshal(f)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		item["restored"] = restored[f.ID]
		item["favorited"] = favorited[f.ID]
		out = append(out, item)
	}
	return out
}
