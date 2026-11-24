package httpapi

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/transfer"
	"fnos-pikpak/internal/websearch"
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
	// Queue schedules download and upload jobs. Handlers still persist the
	// job; the queue only executes it.
	Queue *transfer.Queue
	// DirGuard answers the fnOS ACL question. Nil falls back to Fnos.
	DirGuard DirGuard
	// Links caches signed download URLs. Nil means the default 10-minute
	// cache; tests may inject one.
	Links *linkCache
	// PlaySecret signs stream tokens. Nil means a random per-process secret,
	// which keeps tests hermetic; production derives one from the store key.
	PlaySecret []byte
	// WebSearch finds pages on the PikPak site for 全网搜索. Nil means the
	// default provider client.
	WebSearch *websearch.Client
	// ShareSessions remembers the pass-code token of every opened share so
	// the browser never sees it. Nil means a fresh in-memory cache.
	ShareSessions *shareSessions
	// Thumbs caches proxied thumbnail bytes. Nil means a fresh in-memory
	// cache with the default 30-minute TTL.
	Thumbs *thumbCache
	// Videos runs the 完整播放 preparation jobs. Nil means a fresh registry:
	// one job per user, results kept for 30 minutes.
	Videos *videoJobs
	// DevUID, when non-zero, lets localhost development skip the gateway
	// identity header. Production never sets it.
	DevUID int
	// Debug exposes the probe/report endpoints and the Debug page. It is off
	// in production: one of those endpoints writes probe files on demand.
	Debug bool
	// Logger receives one line per request. Nil means slog's default logger.
	Logger *slog.Logger
}

// NewMux builds the API surface. Every /api route requires a gateway identity.
func NewMux(deps Deps) *http.ServeMux {
	inner := http.NewServeMux()
	if deps.Accounts == nil {
		deps.Accounts = newAccountCache(defaultAccountTTL)
	}
	if deps.Links == nil {
		deps.Links = newLinkCache(linkTTL)
	}
	if deps.ShareSessions == nil {
		deps.ShareSessions = newShareSessions()
	}
	if deps.Thumbs == nil {
		deps.Thumbs = newThumbCache()
	}
	if deps.Videos == nil {
		deps.Videos = newVideoJobs()
	}
	if len(deps.PlaySecret) == 0 {
		secret := make([]byte, 32)
		if _, err := crand.Read(secret); err != nil {
			panic(err)
		}
		deps.PlaySecret = secret
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

	if deps.Debug && deps.Probes != nil {
		registerProbeRoutes(inner, deps, newReportStore())
	}
	if deps.Debug && deps.Fnos != nil {
		registerFSProbe(inner, deps)
	}
	if deps.Store != nil {
		registerFSFolderRoutes(inner, deps)
	}
	if deps.Sessions != nil {
		registerAuthRoutes(inner, deps)
		registerAccountRoutes(inner, deps)
		registerAccountsRoute(inner, deps)
		registerFileRoutes(inner, deps)
		registerUploadRoutes(inner, deps)
		registerTransferRoutes(inner, deps)
		registerTaskRoutes(inner, deps)
		registerShareRoutes(inner, deps)
		registerThumbRoutes(inner, deps)
		registerPlayRoutes(inner, deps)
		registerVideoRoutes(inner, deps)
	}
	registerGatewayRoutes(inner)
	if deps.Debug {
		registerStreamRoutes(inner)
	}
	if deps.Store != nil {
		registerSettingsRoutes(inner, deps)
		registerVideoFavoriteRoutes(inner, deps)
		registerBackupRoutes(inner, deps)
		registerWebSearchRoutes(inner, deps)
		registerDiagRoutes(inner, deps)
	}

	inner.Handle("/", spaHandler())

	root := http.NewServeMux()
	// The forged-header test must see duplicate identity headers, which the
	// identity middleware rejects, so it is registered outside that middleware.
	forged := forgedTest()
	root.HandleFunc("POST /api/gateway/forged", forged)
	root.HandleFunc("POST "+gatewayPrefix+"/api/gateway/forged", forged)

	// Streaming is authenticated by its signed token, not by the gateway
	// header, because a <video> element cannot send one.
	registerStreamRoutesPlay(root, deps)
	registerShareStreamRoutes(root, deps)

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	root.Handle("/", stripGatewayPrefix(gatewayPrefix, fnos.WithIdentity(deps.DevUID, withRequestLog(inner, logger))))
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
