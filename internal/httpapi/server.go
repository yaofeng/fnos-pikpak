package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"

	"fnos-pikpak/internal/fnos"
)

// Deps carries everything the HTTP layer needs from the rest of the app.
type Deps struct {
	Version string
	// DevUID, when non-zero, lets localhost development skip the gateway
	// identity header. Production never sets it.
	DevUID int
}

// NewMux builds the API surface. Every /api route requires a gateway identity.
func NewMux(deps Deps) *http.ServeMux {
	inner := http.NewServeMux()

	inner.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"version": deps.Version,
			"uid":     id.UID,
		})
	})

	inner.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "接口不存在")
	})

	root := http.NewServeMux()
	root.Handle("/", fnos.WithIdentity(deps.DevUID, inner))
	return root
}

// Start listens on a Unix socket with 0600 permissions and serves h.
func Start(socketPath string, h http.Handler) (stop func() error, err error) {
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}

	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()

	stop = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(ctx)
		_ = srv.Close()
		_ = os.Remove(socketPath)
		return err
	}
	return stop, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, message string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{"code": errCode, "message": message},
	})
}
