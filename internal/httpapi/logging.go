package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"fnos-pikpak/internal/fnos"
)

// statusRecorder remembers the status code and passes Flush through so the
// stream proxy can still flush to the player.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withRequestLog writes one JSON line per request: method, matched route,
// status, uid and duration. It logs the mux pattern, never the raw path, so a
// signed playback token cannot end up in the log.
func withRequestLog(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		route := r.Pattern
		if route == "" {
			route = "-"
		}
		uid := 0
		if id, ok := fnos.IdentityFrom(r.Context()); ok {
			uid = id.UID
		}
		logger.Info("request",
			"method", r.Method,
			"route", route,
			"status", rec.status,
			"uid", uid,
			"dur_ms", time.Since(start).Milliseconds(),
		)
	})
}
