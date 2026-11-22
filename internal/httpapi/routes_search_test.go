package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// searchDrive serves a three-level tree: root holds "Movies" and a file,
// Movies holds "Season 1" and one file, Season 1 holds one file.
func searchDrive(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string][]map[string]any{
		"": {
			{"id": "a", "parent_id": "", "name": "Movies", "kind": "drive#folder"},
			{"id": "f0", "parent_id": "", "name": "Movie.2024.mkv", "kind": "drive#file", "size": "10"},
		},
		"a": {
			{"id": "b", "parent_id": "a", "name": "Season 1", "kind": "drive#folder"},
			{"id": "f1", "parent_id": "a", "name": "movie extra.txt", "kind": "drive#file", "size": "2"},
			{"id": "f2", "parent_id": "a", "name": "other.bin", "kind": "drive#file", "size": "3"},
		},
		"b": {
			{"id": "f3", "parent_id": "b", "name": "MOVIE notes.md", "kind": "drive#file", "size": "1"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files":           files[r.URL.Query().Get("parent_id")],
			"next_page_token": "",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFileSearchReturnsHitsWithTheirFolderPath(t *testing.T) {
	srv := searchDrive(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files/search?q=movie", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["scanned"] != float64(6) {
		t.Fatalf("scanned = %v", body["scanned"])
	}
	if body["truncated"] != false {
		t.Fatalf("truncated = %v", body["truncated"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("items = %v", items)
	}
	var deep map[string]any
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == "f3" {
			deep = item
		}
	}
	if deep == nil {
		t.Fatalf("the deep hit is missing: %v", items)
	}
	path, _ := deep["path"].([]any)
	if len(path) != 2 {
		t.Fatalf("path = %v", deep["path"])
	}
	first, _ := path[0].(map[string]any)
	if first["name"] != "Movies" {
		t.Fatalf("path[0] = %v", first)
	}
}

func TestFileSearchNeedsAQuery(t *testing.T) {
	srv := searchDrive(t)
	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files/search?q=%20", "1000")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	errBody, _ := body["error"].(map[string]any)
	if errBody["code"] != "empty_query" {
		t.Fatalf("code = %v", errBody["code"])
	}
}
