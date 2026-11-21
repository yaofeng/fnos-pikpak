// Package fnostest provides a fake fnOS Open API gateway over a Unix socket.
package fnostest

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// StartGateway serves POST /api/v1/trimapp on a temporary Unix socket. The
// handler receives the envelope's data object and returns the data JSON plus
// the envelope code. Any Authorization other than "Bearer test-token" gets 401.
func StartGateway(t *testing.T, handler func(req string, data map[string]any) (code int, dataJSON string)) (socketPath string) {
	t.Helper()

	// A Unix socket path must fit in sun_path (~104 bytes on macOS). t.TempDir
	// embeds the test name under a long TMPDIR, so use a short directory.
	dir, err := os.MkdirTemp("", "fngw")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath = filepath.Join(dir, "gw.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var env struct {
			ReqID   string         `json:"reqId"`
			Req     string         `json:"req"`
			AppName string         `json:"appName"`
			Data    map[string]any `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		code, data := handler(env.Req, env.Data)
		resp := map[string]any{
			"reqId":   env.ReqID,
			"code":    code,
			"msg":     "",
			"appName": env.AppName,
		}
		if data != "" {
			resp["data"] = json.RawMessage(data)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	return socketPath
}

// StartSlowGateway answers after a fixed delay, for timeout tests.
func StartSlowGateway(t *testing.T) string {
	t.Helper()
	return StartGateway(t, func(string, map[string]any) (int, string) {
		time.Sleep(2 * time.Second)
		return 0, `{}`
	})
}
