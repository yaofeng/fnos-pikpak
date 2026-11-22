package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// thumbUpstream serves one drive file with a thumbnail and one share whose
// file entry carries a thumbnail.
func thumbUpstream(t *testing.T) (*httptest.Server, *httptest.Server) {
	t.Helper()
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "JPEG")
	}))
	t.Cleanup(images.Close)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v1/files/f1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "f1", "name": "a.jpg", "kind": "drive#file", "size": "9",
				"thumbnail_link": images.URL + "/f1.jpg",
			})
		case "/drive/v1/files/f2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "f2", "name": "b.bin", "kind": "drive#file", "size": "9",
			})
		case "/drive/v1/share":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"share_status": "OK", "pass_code_token": "tok-1", "title": "资料",
				"files": []map[string]any{{"id": "sf2", "name": "notes.mp4", "kind": "drive#file", "size": "9"}},
			})
		case "/drive/v1/share/file_info":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_info": map[string]any{
					"id": "sf2", "name": "notes.mp4", "size": "9",
					"web_content_link": images.URL + "/sf2.mp4",
					"thumbnail_link":   images.URL + "/sf2.jpg",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, images
}

func thumbGet(t *testing.T, h http.Handler, path, uid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if uid != "" {
		req.Header.Set("X-Trim-Userid", uid)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestThumbProxiesDriveImageBytes(t *testing.T) {
	srv, images := thumbUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec := thumbGet(t, h, "/api/thumb?file_id=f1", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if rec.Body.String() != "JPEG" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), images.URL) {
		t.Fatal("the CDN URL reached the browser")
	}
}

func TestThumbUsesTheShareSessionForShareFiles(t *testing.T) {
	srv, _ := thumbUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	missing := thumbGet(t, h, "/api/thumb?share_id=VOabc123&file_id=sf2", "1000")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("without session: status = %d body=%s", missing.Code, missing.Body.String())
	}

	if rec, body := getJSON(t, h, "/api/share?url=VOabc123&pass_code=7f3a", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("open share: %d %v", rec.Code, body)
	}
	rec := thumbGet(t, h, "/api/thumb?share_id=VOabc123&file_id=sf2", "1000")
	if rec.Code != http.StatusOK || rec.Body.String() != "JPEG" {
		t.Fatalf("share thumb: status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestThumbReturns404WhenTheFileHasNoThumbnail(t *testing.T) {
	srv, _ := thumbUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec := thumbGet(t, h, "/api/thumb?file_id=f2", "1000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
