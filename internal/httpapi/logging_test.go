package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogNamesTheRouteAndHidesTheToken(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stream/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stream/secret-token", nil)
	rec := httptest.NewRecorder()
	withRequestLog(mux, logger).ServeHTTP(rec, req)

	line := buf.String()
	if strings.Contains(line, "secret-token") {
		t.Fatalf("the playback token leaked into the log: %s", line)
	}
	if !strings.Contains(line, `"route":"GET /api/stream/{token}"`) {
		t.Fatalf("route missing: %s", line)
	}
	if !strings.Contains(line, `"status":206`) {
		t.Fatalf("status missing: %s", line)
	}
}
