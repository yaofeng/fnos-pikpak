package pikpak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A file the user deleted is only in the trash: PikPak's listing still returns
// it unless the request excludes trashed entries, and every ListAllFiles caller
// asks "does this file exist?", so a trashed copy must never count.
func TestListAllFilesAsksForLiveFilesOnly(t *testing.T) {
	var filters []map[string]any
	pages := []string{
		`{"files":[{"id":"live","kind":"drive#file","name":"a.mp4","size":"10"},{"id":"gone","kind":"drive#file","name":"b.mp4","size":"20","trashed":true}],"next_page_token":"p2"}`,
		`{"files":[{"id":"gone2","kind":"drive#file","name":"c.mp4","size":"30","trashed":true}],"next_page_token":""}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var parsed map[string]any
		_ = json.Unmarshal([]byte(r.URL.Query().Get("filters")), &parsed)
		filters = append(filters, parsed)
		page := pages[0]
		if r.URL.Query().Get("page_token") == "p2" {
			page = pages[1]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	files, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).ListAllFiles(context.Background(), "pack")
	if err != nil {
		t.Fatalf("ListAllFiles: %v", err)
	}
	if len(files) != 1 || files[0].ID != "live" {
		t.Fatalf("files = %+v, want only the live one", files)
	}
	if len(filters) != 2 {
		t.Fatalf("asked %d pages, want 2", len(filters))
	}
	for i, f := range filters {
		trashed, _ := f["trashed"].(map[string]any)
		if trashed == nil || trashed["eq"] != false {
			t.Fatalf("page %d filters = %v, want trashed eq false", i, f)
		}
	}
}
