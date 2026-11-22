package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"os"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

var errUnsupportedMediaType = errors.New("httpapi: unsupported media type")

// Deps carries everything the HTTP layer needs from the rest of the app.
type Deps struct {
	Version  string
	Probes   *probe.Registry
	Fnos     *fnos.Client
	Store    *store.Store
	Sessions *pikpak.SessionStore
	// Accounts caches per-user quota/VIP summaries. Nil means the default
	// 60-second cache; tests inject a shorter one.
	Accounts *accountCache
	// DevUID, when non-zero, lets localhost development skip the gateway
	// identity header. Production never sets it.
	DevUID int
}

// NewMux builds the API surface. Every /api route requires a gateway identity.
func NewMux(deps Deps) *http.ServeMux {
	inner := http.NewServeMux()
	if deps.Accounts == nil {
		deps.Accounts = newAccountCache(defaultAccountTTL)
	}

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

	if deps.Probes != nil {
		registerProbeRoutes(inner, deps, newReportStore())
	}
	if deps.Fnos != nil {
		registerFSRoutes(inner, deps)
	}
	if deps.Sessions != nil {
		registerAuthRoutes(inner, deps)
		registerAccountRoutes(inner, deps)
		registerFileRoutes(inner, deps)
		registerUploadRoutes(inner, deps)
	}
	registerGatewayRoutes(inner)
	registerStreamRoutes(inner)
	if deps.Store != nil {
		registerSettingsRoutes(inner, deps)
	}

	inner.Handle("/", spaHandler())

	root := http.NewServeMux()
	// The forged-header test must see duplicate identity headers, which the
	// identity middleware rejects, so it is registered outside that middleware.
	forged := forgedTest()
	root.HandleFunc("POST /api/gateway/forged", forged)
	root.HandleFunc("POST "+gatewayPrefix+"/api/gateway/forged", forged)

	root.Handle("/", stripGatewayPrefix(gatewayPrefix, fnos.WithIdentity(deps.DevUID, inner)))
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

// decodeJSON reads a small JSON body and writes a 400 on failure.
// requireJSONBody is the CSRF barrier for state-changing endpoints: a
// cross-site form cannot declare application/json without a CORS preflight.
func requireJSONBody(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "请求必须使用 application/json")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if !requireJSONBody(w, r) {
		return errUnsupportedMediaType
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
		return err
	}
	return nil
}
