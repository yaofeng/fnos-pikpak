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
			// Protect the share so a thumbnail can only be fetched by a caller
			// that knows the code; tests always pass 7f3a.
			if r.URL.Query().Get("pass_code") != "7f3a" {
				_ = json.NewEncoder(w).Encode(map[string]any{"share_status": "PASS_CODE_EMPTY"})
				return
			}
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

// 视频收藏夹 never opens the share, so the first thumbnail request has no
// session; the saved code must be enough to reopen it.
func TestThumbReopensTheShareFromASavedVideo(t *testing.T) {
	srv, images := thumbUpstream(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"VOabc123","file_id":"sf2","name":"notes.mp4","size":9,"pass_code":"7f3a"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("favorite: %d %v", rec.Code, body)
	}
	thumb := thumbGet(t, h, "/api/thumb?share_id=VOabc123&file_id=sf2", "1000")
	if thumb.Code != http.StatusOK || thumb.Body.String() != "JPEG" {
		t.Fatalf("thumb: status=%d body=%s", thumb.Code, thumb.Body.String())
	}
	if strings.Contains(thumb.Body.String(), images.URL) {
		t.Fatal("the CDN URL reached the browser")
	}
}

// A public share needs no pass code, so a saved video without one must still
// get its thumbnail; only a protected share with no code is a dead end.
func TestThumbOpensAPublicShareWithoutACode(t *testing.T) {
	images := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "JPEG")
	}))
	t.Cleanup(images.Close)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v1/share":
			if got := r.URL.Query().Get("pass_code"); got != "" {
				t.Errorf("public share asked with pass_code %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"share_status": "OK", "pass_code_token": "tok-pub", "title": "公开",
				"files": []map[string]any{{"id": "sf9", "name": "open.mp4", "kind": "drive#file", "size": "9"}},
			})
		case "/drive/v1/share/file_info":
			_ = json.NewEncoder(w).Encode(map[string]any{"file_info": map[string]any{
				"id": "sf9", "name": "open.mp4", "size": "9",
				"web_content_link": images.URL + "/open.mp4",
				"thumbnail_link":   images.URL + "/open.jpg",
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/video-favorites", "1000",
		`{"share_id":"PUBLIC01","file_id":"sf9","name":"open.mp4","size":9}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("favorite: %d %v", rec.Code, body)
	}
	thumb := thumbGet(t, h, "/api/thumb?share_id=PUBLIC01&file_id=sf9", "1000")
	if thumb.Code != http.StatusOK || thumb.Body.String() != "JPEG" {
		t.Fatalf("thumb: status=%d body=%s", thumb.Code, thumb.Body.String())
	}
}
