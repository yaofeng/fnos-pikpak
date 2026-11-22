package httpapi

import (
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
		var body struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		name := strings.TrimSpace(body.Name)
		if utf8.RuneCountInString(name) > 100 {
			writeError(w, http.StatusBadRequest, "bad_name", "显示名太长")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		fav, err := deps.Store.RenameVideoFavorite(r.Context(), id.UID, favID, name)
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
