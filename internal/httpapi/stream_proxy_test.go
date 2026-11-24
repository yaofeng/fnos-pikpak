package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/playtoken"
)

const mediaSize = 1024

var mediaBody = func() []byte {
	b := make([]byte, mediaSize)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}()

type streamEnv struct {
	deps       Deps
	linkCalls  int32
	expireNext bool
}

// newStreamEnv serves a downloadable file plus two media URLs: one that
// answers 403 (an expired signature) and one that supports ranges.
func newStreamEnv(t *testing.T, expiredFirst bool) *streamEnv {
	t.Helper()
	env := &streamEnv{}
	mux := http.NewServeMux()

	var medias *httptest.Server
	mux.HandleFunc("GET /drive/v1/files/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("usage") != "FETCH" {
			_, _ = io.WriteString(w, `{"id":"f1","name":"a.mp4","kind":"drive#file","size":"1024","mime_type":"video/mp4"}`)
			return
		}
		n := atomic.AddInt32(&env.linkCalls, 1)
		if expiredFirst && n == 1 {
			fmt.Fprintf(w, `{"web_content_link":%q,"size":"1024"}`, medias.URL+"/media/expired")
			return
		}
		fmt.Fprintf(w, `{"web_content_link":%q,"size":"1024"}`, medias.URL+"/media/ok")
	})
	mux.HandleFunc("GET /media/expired", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	mux.HandleFunc("/media/ok", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "a.mp4", time.Unix(1_700_000_000, 0), bytes.NewReader(mediaBody))
	})
	medias = httptest.NewServer(mux)
	t.Cleanup(medias.Close)

	deps, st := newAuthTestDeps(t, medias.URL)
	bindTestUser(t, st, 1000)
	deps.PlaySecret = []byte("test-play-secret-test-play-secret")
	env.deps = deps
	return env
}

func (e *streamEnv) request(t *testing.T, method string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	token := playtoken.Sign(e.deps.PlaySecret, 1000, "", "f1", time.Hour, time.Now())
	req := httptest.NewRequest(method, "/api/stream/f1?t="+token, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	NewMux(e.deps).ServeHTTP(rec, req)
	return rec
}

func TestStreamFullGETIsProxied(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodGet, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), mediaBody) {
		t.Fatalf("body length = %d", rec.Body.Len())
	}
}

func TestStreamRangeRequestReturns206(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodGet, map[string]string{"Range": "bytes=0-99"})

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 0-99/1024" {
		t.Fatalf("Content-Range = %q", got)
	}
	if rec.Body.Len() != 100 {
		t.Fatalf("body length = %d", rec.Body.Len())
	}
	if !bytes.Equal(rec.Body.Bytes(), mediaBody[:100]) {
		t.Fatal("range body does not match")
	}
}

func TestStreamOpenRangeReturnsRemainder(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodGet, map[string]string{"Range": "bytes=1000-"})

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 1000-1023/1024" {
		t.Fatalf("Content-Range = %q", got)
	}
	if rec.Body.Len() != 24 {
		t.Fatalf("body length = %d", rec.Body.Len())
	}
}

func TestStreamRangeBeyondEOFIs416(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodGet, map[string]string{"Range": "bytes=2000-2100"})

	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes */1024" {
		t.Fatalf("Content-Range = %q", got)
	}
}

func TestStreamHeadHasNoBody(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodHead, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD returned %d bytes", rec.Body.Len())
	}
}

func TestStreamRefreshesLinkOnForbidden(t *testing.T) {
	env := newStreamEnv(t, true)
	rec := env.request(t, http.MethodGet, map[string]string{"Range": "bytes=0-9"})

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if got := atomic.LoadInt32(&env.linkCalls); got != 2 {
		t.Fatalf("link calls = %d, want 2 (one refresh)", got)
	}
	if !bytes.Equal(rec.Body.Bytes(), mediaBody[:10]) {
		t.Fatal("body mismatch after refresh")
	}
}

func TestStreamRedirectMode(t *testing.T) {
	env := newStreamEnv(t, false)
	if err := env.deps.Store.SetSetting(t.Context(), "play_mode", "redirect"); err != nil {
		t.Fatal(err)
	}
	rec := env.request(t, http.MethodGet, nil)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Fatal("redirect without Location")
	}
}

func TestStreamRejectsPost(t *testing.T) {
	env := newStreamEnv(t, false)
	rec := env.request(t, http.MethodPost, nil)
	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}
