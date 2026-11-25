package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"fnos-pikpak/internal/fnos"
)

// registerVideoRoutes serves 完整播放 and 预览. Preparing a video may take a
// minute (restore, purge, move), so it runs as a background job the browser
// polls; the preview is one signed URL and answers directly.
func registerVideoRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /api/videos/prepare", func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeVideoRequest(w, r)
		if !ok {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if _, err := deps.Sessions.ClientForAccount(r.Context(), id.UID, ""); err != nil {
			writeVideoError(w, err)
			return
		}
		jobID, started := deps.Videos.Start(id.UID, func(ctx context.Context) (videoPrepareResult, error) {
			return prepareVideo(ctx, deps, id.UID, req)
		})
		if !started {
			writeError(w, http.StatusConflict, "video_busy", "上一个视频还在准备，请等它播完")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
	})

	mux.HandleFunc("GET /api/videos/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		job, ok := deps.Videos.Get(id.UID, r.PathValue("id"), time.Now())
		if !ok {
			writeError(w, http.StatusNotFound, "job_not_found", "任务不存在或已过期")
			return
		}
		writeJSON(w, http.StatusOK, job)
	})

	mux.HandleFunc("POST /api/videos/preview", func(w http.ResponseWriter, r *http.Request) {
		req, ok := decodeVideoRequest(w, r)
		if !ok {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		got, err := previewShareVideo(r.Context(), deps, id.UID, req)
		if err != nil {
			writeVideoError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	})
}

// decodeVideoRequest reads the two ways a video is named: a saved favorite, or
// a share file from a tab whose session is already open.
func decodeVideoRequest(w http.ResponseWriter, r *http.Request) (videoPrepareRequest, bool) {
	var body struct {
		ShareID    string `json:"share_id"`
		FileID     string `json:"file_id"`
		FavoriteID int64  `json:"video_favorite_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return videoPrepareRequest{}, false
	}
	req := videoPrepareRequest{
		ShareID:    strings.TrimSpace(body.ShareID),
		FileID:     strings.TrimSpace(body.FileID),
		FavoriteID: body.FavoriteID,
	}
	if req.FavoriteID <= 0 && (req.ShareID == "" || req.FileID == "") {
		writeError(w, http.StatusBadRequest, "bad_ids", "缺少分享或文件编号")
		return videoPrepareRequest{}, false
	}
	if req.FavoriteID < 0 {
		writeError(w, http.StatusBadRequest, "bad_ids", "收藏编号不合法")
		return videoPrepareRequest{}, false
	}
	return req, true
}

// writeVideoError maps the worker's errors onto the codes the browser knows.
func writeVideoError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotBound):
		writeError(w, http.StatusUnauthorized, "not_bound", err.Error())
	case errors.Is(err, errSessionGone):
		writeError(w, http.StatusBadRequest, "share_session_missing", err.Error())
	case errors.Is(err, errFavoriteMissing):
		writeError(w, http.StatusNotFound, "favorite_not_found", err.Error())
	case errors.Is(err, errPassRequired):
		writeError(w, http.StatusBadRequest, "pass_code_required", err.Error())
	case errors.Is(err, errPassWrong):
		writeError(w, http.StatusBadRequest, "pass_code_wrong", err.Error())
	case errors.Is(err, errShareUnavailable):
		writeError(w, http.StatusNotFound, "share_unavailable", err.Error())
	case errors.Is(err, errReauth):
		writeError(w, http.StatusUnauthorized, "pikpak_reauth_required", err.Error())
	case errors.Is(err, errAllAccountsFailed):
		writeError(w, http.StatusTooManyRequests, "all_accounts_failed", err.Error())
	case errors.Is(err, errSpaceInsufficient), errors.Is(err, errUpstream), errors.Is(err, errSessionLoad),
		errors.Is(err, errVideoNoLink):
		writeError(w, http.StatusBadGateway, "video_failed", err.Error())
	default:
		writeError(w, http.StatusBadGateway, "video_failed", "视频准备失败，请稍后重试")
	}
}
