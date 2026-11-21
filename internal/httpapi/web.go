package httpapi

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"fnos-pikpak/internal/webdist"
)

const gatewayPrefix = "/app/pikpak"

// stripGatewayPrefix makes routes work whether the fnOS gateway forwards the
// public path as-is or strips it.
func stripGatewayPrefix(prefix string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, prefix+"/") {
			r = r.Clone(r.Context())
			r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
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
		r = r.Clone(r.Context())
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
