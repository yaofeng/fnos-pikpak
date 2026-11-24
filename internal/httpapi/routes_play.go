package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/probe"
)

// playTokenTTL is how long a stream URL stays usable. Six hours covers a long
// movie with room to seek; a new URL is issued every time the page opens.
const playTokenTTL = 6 * time.Hour

func registerPlayRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/play/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		client, _, ok := accountClient(w, r, deps)
		if !ok {
			return
		}
		file, err := client.FileDetail(r.Context(), r.PathValue("id"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if file.Kind == "drive#folder" {
			writeError(w, http.StatusBadRequest, "not_a_file", "文件夹不能播放")
			return
		}
		// TODO(多账号): Task 8 接入真实账号
		token := playtoken.Sign(deps.PlaySecret, id.UID, "", file.ID, playTokenTTL, time.Now())
		writeJSON(w, http.StatusOK, map[string]any{
			"file_id":    file.ID,
			"name":       file.Name,
			"mime":       file.MimeType,
			"size":       file.Size,
			"stream_url": "/api/stream/" + url.PathEscape(file.ID) + "?t=" + token,
		})
	})
}

// registerStreamRoutesPlay wires the stream endpoint outside the identity
// middleware: a <video> element cannot add headers, so the signed token is the
// only identity this route accepts.
func registerStreamRoutesPlay(root *http.ServeMux, deps Deps) {
	for _, prefix := range []string{"", gatewayPrefix} {
		// Registered without a method so a stray POST gets 405 here instead of
		// falling into the identity middleware and answering 401.
		root.HandleFunc(prefix+"/api/stream/{id}", deps.streamFile)
	}
}

func (deps Deps) streamFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET 和 HEAD")
		return
	}
	if deps.Sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "PikPak 会话未配置")
		return
	}
	claims, err := playtoken.Verify(deps.PlaySecret, r.URL.Query().Get("t"), time.Now())
	if errors.Is(err, playtoken.ErrExpired) {
		writeError(w, http.StatusUnauthorized, "token_expired", "播放链接已过期，请重新打开")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "播放链接无效")
		return
	}
	if claims.FileID != r.PathValue("id") || claims.ShareID != "" {
		// A share token names a share as well; it must not open a drive file.
		writeError(w, http.StatusUnauthorized, "invalid_token", "播放链接与文件不匹配")
		return
	}

	if playMode(r.Context(), deps, claims.UID) == "redirect" {
		url, err := deps.linkFor(r.Context(), claims.UID, claims.FileID, false)
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
		return
	}
	deps.proxyStream(w, r, claims.UID, claims.FileID)
}
