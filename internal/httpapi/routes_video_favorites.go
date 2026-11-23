package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
)

// registerVideoFavoriteRoutes serves the saved-video list. The pass code is
// accepted once, sealed, and never returned.
func registerVideoFavoriteRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/video-favorites", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		items, err := deps.Store.ListVideoFavorites(r.Context(), id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "读取视频收藏失败")
			return
		}
		markRestoredCopies(r.Context(), deps, id.UID, items)
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.HandleFunc("POST /api/video-favorites", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ShareID    string `json:"share_id"`
			FileID     string `json:"file_id"`
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			Owner      string `json:"owner"`
			ShareTitle string `json:"share_title"`
			PassCode   string `json:"pass_code"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		shareID := strings.TrimSpace(body.ShareID)
		fileID := strings.TrimSpace(body.FileID)
		if shareID == "" || fileID == "" {
			writeError(w, http.StatusBadRequest, "bad_ids", "缺少分享或文件编号")
			return
		}
		if body.Size < 0 {
			writeError(w, http.StatusBadRequest, "bad_size", "文件大小不合法")
			return
		}
		name := strings.TrimSpace(body.Name)
		if utf8.RuneCountInString(name) > 255 {
			writeError(w, http.StatusBadRequest, "bad_name", "文件名太长")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		// A video starred from a share opened through 分享收藏夹 arrives
		// without its code; the saved share already holds it, sealed.
		passCode := strings.TrimSpace(body.PassCode)
		if passCode == "" {
			if code, err := deps.Store.SharePassCode(r.Context(), id.UID, shareID); err == nil {
				passCode = code
			}
		}
		fav, err := deps.Store.AddVideoFavorite(r.Context(), id.UID, store.VideoFavorite{
			ShareID:    shareID,
			FileID:     fileID,
			Name:       name,
			Size:       body.Size,
			Owner:      strings.TrimSpace(body.Owner),
			ShareTitle: strings.TrimSpace(body.ShareTitle),
			PassCode:   passCode,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "保存视频收藏失败")
			return
		}
		writeJSON(w, http.StatusCreated, fav)
	})

	mux.HandleFunc("PATCH /api/video-favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		favID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
			return
		}
		// A missing field keeps what is stored; an empty string clears it
		// (the original name, or the sharing user's group).
		var body struct {
			Name       *string `json:"name"`
			GroupLabel *string `json:"group_label"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		var fav store.VideoFavorite
		if body.Name != nil {
			name := strings.TrimSpace(*body.Name)
			if utf8.RuneCountInString(name) > 100 {
				writeError(w, http.StatusBadRequest, "bad_name", "显示名太长")
				return
			}
			fav, err = deps.Store.RenameVideoFavorite(r.Context(), id.UID, favID, name)
			if err != nil {
				writeVideoFavoriteError(w, err, "重命名失败")
				return
			}
		}
		if body.GroupLabel != nil {
			label := strings.TrimSpace(*body.GroupLabel)
			if utf8.RuneCountInString(label) > 100 {
				writeError(w, http.StatusBadRequest, "bad_name", "分组名太长")
				return
			}
			fav, err = deps.Store.MoveVideoFavoriteGroup(r.Context(), id.UID, favID, label)
			if err != nil {
				writeVideoFavoriteError(w, err, "移动分组失败")
				return
			}
		}
		if body.Name == nil && body.GroupLabel == nil {
			writeError(w, http.StatusBadRequest, "bad_request", "没有要修改的字段")
			return
		}
		writeJSON(w, http.StatusOK, fav)
	})

	// Renaming a group is local: every saved video currently grouped under
	// from_key gets the new label.
	mux.HandleFunc("PATCH /api/video-favorites/groups", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FromKey string `json:"from_key"`
			Label   string `json:"label"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		fromKey := strings.TrimSpace(body.FromKey)
		label := strings.TrimSpace(body.Label)
		if fromKey == "" || utf8.RuneCountInString(fromKey) > 200 {
			writeError(w, http.StatusBadRequest, "bad_ids", "分组不存在")
			return
		}
		if label == "" || utf8.RuneCountInString(label) > 100 {
			writeError(w, http.StatusBadRequest, "bad_name", "分组名不能为空或太长")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		moved, err := deps.Store.RenameVideoGroup(r.Context(), id.UID, fromKey, label)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "重命名分组失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "moved": moved})
	})

	mux.HandleFunc("DELETE /api/video-favorites/{id}", func(w http.ResponseWriter, r *http.Request) {
		favID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || favID <= 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if err := deps.Store.DeleteVideoFavorite(r.Context(), id.UID, favID); err != nil {
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

// writeVideoFavoriteError maps the store's one sentinel onto the same 404 the
// rename and move paths answer with.
func writeVideoFavoriteError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "favorite_not_found", "收藏不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", fallback)
}

// markRestoredCopies sets Restored on the favourites whose copy from an earlier
// 完整播放 still exists — in Pack From Shared, or the root for older accounts.
// One listing answers for the whole page. The badge claims a copy exists, so
// when the drive cannot be read it stays off: the stored id says where a copy
// once was, not that it survived a later delete.
func markRestoredCopies(ctx context.Context, deps Deps, uid int, items []store.VideoFavorite) {
	wanted := false
	for _, fav := range items {
		if fav.RestoredFileID != "" {
			wanted = true
			break
		}
	}
	if !wanted {
		return
	}
	client, err := deps.Sessions.ClientFor(ctx, uid)
	if err != nil {
		return
	}
	root, err := client.ListAllFiles(ctx, "")
	if err != nil {
		return
	}
	present := fileIDs(root)
	if pack, ok := findFolder(root, packFromSharedName); ok {
		files, err := client.ListAllFiles(ctx, pack.ID)
		if err != nil {
			return
		}
		for id := range fileIDs(files) {
			present[id] = true
		}
	}
	for i := range items {
		items[i].Restored = items[i].RestoredFileID != "" && present[items[i].RestoredFileID]
	}
}
