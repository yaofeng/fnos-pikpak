package httpapi

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"fnos-pikpak/internal/webdist"
)

const gatewayPrefix = "/app/pikpak"

type originalPathKey struct{}

// OriginalPath returns the request path before the gateway prefix was
// stripped, so diagnostics can report what the gateway actually forwarded.
func OriginalPath(ctx context.Context) string {
	p, _ := ctx.Value(originalPathKey{}).(string)
	return p
}

// stripGatewayPrefix makes routes work whether the fnOS gateway forwards the
// public path as-is or strips it. The bare prefix is redirected to prefix + "/"
// so the document's relative asset URLs resolve inside the app: the gateway
// serves the app at exactly /app/pikpak, and without the slash "./assets/x.js"
// would resolve to /assets/x.js and never load.
func stripGatewayPrefix(prefix string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix {
			target := prefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), originalPathKey{}, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, prefix+"/") {
			r = r.Clone(ctx)
			r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		} else {
			r = r.Clone(ctx)
		}
		h.ServeHTTP(w, r)
	})
}

// spaHandler serves embedded files and falls back to index.html.
func spaHandler() http.Handler {
	root, err := fs.Sub(webdist.FS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(root, p); err == nil && !st.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// A missing file is a 404, not the SPA document: answering an image
		// request with HTML only hides the cause behind a broken image.
		if path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
