package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"fnos-pikpak/internal/stream"
)

const (
	defaultProbeSize = int64(512 << 20)
	maxProbeSize     = int64(2 << 30)
)

func registerStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stream-probe", func(w http.ResponseWriter, r *http.Request) {
		size := defaultProbeSize
		if raw := r.URL.Query().Get("size"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed <= 0 {
				writeError(w, http.StatusBadRequest, "bad_size", "size 必须是正整数")
				return
			}
			size = parsed
		}
		if size > maxProbeSize {
			writeError(w, http.StatusBadRequest, "size_too_large", "size 不能超过 2 GiB")
			return
		}

		dir := os.Getenv("TRIM_PKGTMP")
		if dir == "" {
			dir = filepath.Join(os.TempDir(), "pikpak-probe")
		}
		path, err := stream.EnsureProbeFile(dir, size)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "probe_file", err.Error())
			return
		}
		stream.ServeFile(w, r, path, size)
	})
}
