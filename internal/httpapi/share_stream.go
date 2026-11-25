package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"fnos-pikpak/internal/playtoken"
	"fnos-pikpak/internal/probe"
)

// registerShareStreamRoutes serves share media outside the identity
// middleware: a <video> element cannot send the gateway header, so the signed
// token is the identity. Download is the same bytes with an attachment header.
func registerShareStreamRoutes(root *http.ServeMux, deps Deps) {
	for _, prefix := range []string{"", gatewayPrefix} {
		root.HandleFunc(prefix+"/api/share/stream/{share}/{file}", deps.shareMedia(false))
		root.HandleFunc(prefix+"/api/share/download/{share}/{file}", deps.shareMedia(true))
	}
}

func (deps Deps) shareMedia(attachment bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if err != nil || claims.ShareID == "" ||
			claims.ShareID != r.PathValue("share") || claims.FileID != r.PathValue("file") {
			writeError(w, http.StatusUnauthorized, "invalid_token", "播放链接无效")
			return
		}
		passToken, _, ok := deps.ShareSessions.get(claims.UID, claims.AccountID, claims.ShareID, time.Now())
		if !ok {
			writeError(w, http.StatusUnauthorized, "share_session_missing", "分享会话已过期，请重新打开分享")
			return
		}
		// The token carries the account that opened the share; the stream is
		// fetched through that account's session, not the default one.
		client, err := deps.Sessions.ClientForAccount(r.Context(), claims.UID, claims.AccountID)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}

		link, err := client.GetShareFileLink(r.Context(), claims.ShareID, claims.FileID, passToken)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if !attachment && playMode(r.Context(), deps, claims.UID) == "redirect" {
			http.Redirect(w, r, link.URL, http.StatusFound)
			return
		}

		resp, err := fetchMedia(r, link.URL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
			// The signature expired: ask for a new URL and retry once.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
			resp.Body.Close()
			link, err = client.GetShareFileLink(r.Context(), claims.ShareID, claims.FileID, passToken)
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			resp, err = fetchMedia(r, link.URL)
			if err != nil {
				writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
				return
			}
		}
		defer resp.Body.Close()

		copyMediaHeaders(w.Header(), resp.Header)
		if attachment {
			name := link.Name
			if name == "" {
				name = claims.FileID
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		}
		w.WriteHeader(resp.StatusCode)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(w, resp.Body)
	}
}
