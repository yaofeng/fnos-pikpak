package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/playtoken"
)

type playEnv struct {
	deps     Deps
	upstream *httptest.Server
}

func newPlayEnv(t *testing.T) playEnv {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /drive/v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("usage") == "FETCH" {
			_, _ = io.WriteString(w, `{"web_content_link":"https://cdn.example.com/secret-signed?token=abc","size":"123"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"f1","name":"a.mp4","kind":"drive#file","size":"123",
			"mime_type":"video/mp4","phase":"PHASE_TYPE_COMPLETE"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	deps, st := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, st, 1000)
	deps.PlaySecret = []byte("test-play-secret-test-play-secret")
	return playEnv{deps: deps, upstream: srv}
}

func (e playEnv) streamTarget(t *testing.T, body map[string]any) string {
	t.Helper()
	streamURL, _ := body["stream_url"].(string)
	if streamURL == "" {
		t.Fatalf("no stream_url in %v", body)
	}
	return streamURL
}

func TestPlayReturnsTokenisedStreamURL(t *testing.T) {
	env := newPlayEnv(t)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/play/f1", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["mime"] != "video/mp4" || body["size"] != float64(123) {
		t.Fatalf("body = %v", body)
	}
	streamURL := env.streamTarget(t, body)
	if !strings.HasPrefix(streamURL, "/api/stream/f1?t=") {
		t.Fatalf("stream_url = %q", streamURL)
	}
	// The upstream signed link must never reach the browser unproxied.
	if strings.Contains(streamURL, "cdn.example.com") || strings.Contains(streamURL, "web_content_link") {
		t.Fatalf("stream_url leaks the upstream link: %q", streamURL)
	}
}

func TestPlayRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/play/f1", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestStreamRejectsForgedToken(t *testing.T) {
	env := newPlayEnv(t)
	h := NewMux(env.deps)

	_, body := getJSON(t, h, "/api/play/f1", "1000")
	streamURL := env.streamTarget(t, body)

	// Flip one data-carrying character of the signature: the very last base64
	// character holds padding bits, and A↔B there decodes to the same MAC.
	cut := len(streamURL) - 2
	tampered := streamURL[:cut] + flipLast(streamURL[:cut+1]) + streamURL[cut+1:]
	for _, target := range []string{tampered, "/api/stream/f1?t=garbage"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		assertErrorCode(t, rec, "invalid_token")
	}
}

func TestStreamRejectsExpiredToken(t *testing.T) {
	env := newPlayEnv(t)
	h := NewMux(env.deps)

	token := playtoken.Sign(env.deps.PlaySecret, 1000, "f1", -time.Hour, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/api/stream/f1?t="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	assertErrorCode(t, rec, "token_expired")
}

func TestStreamRejectsTokenForAnotherFile(t *testing.T) {
	env := newPlayEnv(t)
	h := NewMux(env.deps)

	token := playtoken.Sign(env.deps.PlaySecret, 1000, "f1", time.Hour, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/api/stream/f2?t="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	assertErrorCode(t, rec, "invalid_token")
}

func TestStreamAcceptsValidTokenWithoutIdentityHeader(t *testing.T) {
	env := newPlayEnv(t)
	h := NewMux(env.deps)

	token := playtoken.Sign(env.deps.PlaySecret, 1000, "f1", time.Hour, time.Now())
	req := httptest.NewRequest(http.MethodGet, "/api/stream/f1?t="+token, nil)
	// No X-Trim-Userid: the signed token is the identity for this route.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("a valid token was rejected: %s", rec.Body)
	}
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "cdn.example.com") {
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d with Location %q", rec.Code, loc)
		}
	}
}

// assertErrorCode reads the standard {"error":{"code":...}} envelope, so a 401
// produced by the identity middleware (a plain error string) does not count.
func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	if body.Error.Code != want {
		t.Fatalf("error code = %q, want %q (%s)", body.Error.Code, want, rec.Body)
	}
}

// flipLast replaces the last character with a different one. The final base64
// character of a 32-byte HMAC carries padding bits, and changing only those
// decodes to the same signature, so callers pass a string cut back by one more
// character: the second-to-last character is all data.
func flipLast(s string) string {
	last := s[len(s)-1]
	if last == 'A' {
		return "B"
	}
	return "A"
}
