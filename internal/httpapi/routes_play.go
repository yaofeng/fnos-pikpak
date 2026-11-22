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
		client, ok := boundClient(w, r, deps)
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
		token := playtoken.Sign(deps.PlaySecret, id.UID, file.ID, playTokenTTL, time.Now())
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
		root.HandleFunc("GET "+prefix+"/api/stream/{id}", deps.streamFile)
		root.HandleFunc("HEAD "+prefix+"/api/stream/{id}", deps.streamFile)
	}
}

func (deps Deps) streamFile(w http.ResponseWriter, r *http.Request) {
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
	if claims.FileID != r.PathValue("id") {
		writeError(w, http.StatusUnauthorized, "invalid_token", "播放链接与文件不匹配")
		return
	}

	client, err := deps.Sessions.ClientFor(r.Context(), claims.UID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not_bound", "PikPak 账号未绑定")
		return
	}
	link, err := client.GetDownloadLink(r.Context(), claims.FileID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
		return
	}
	if link.URL == "" {
		writeError(w, http.StatusBadGateway, "pikpak_error", "PikPak 没有返回下载地址")
		return
	}
	// Task 19 replaces this redirect with a Range proxy; the signed URL stays
	// server-side once proxying lands.
	http.Redirect(w, r, link.URL, http.StatusFound)
}
