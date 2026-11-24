package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"fnos-pikpak/internal/pikpak"
)

func TestPackCopiesMatchRules(t *testing.T) {
	listings := []packListing{
		{AccountID: "acct-a", Pack: []pikpak.File{
			{ID: "a-copy", Kind: "drive#file", Name: "X", Size: 100},
		}},
		{AccountID: "acct-b", Root: []pikpak.File{
			{ID: "b-root", Kind: "drive#file", Name: "root.bin", Size: 7},
		}, Pack: []pikpak.File{
			{ID: "b-copy", Kind: "drive#file", Name: "X", Size: 200},
			{ID: "b-dir", Kind: "drive#folder", Name: "X"},
		}},
	}

	// Exact name and size matches a's copy only.
	f, acct, ok := findCopyAcross(listings, "", "X", "X", 100)
	if !ok || acct != "acct-a" || f.ID != "a-copy" {
		t.Fatalf("size match = %s in %s ok=%v", f.ID, acct, ok)
	}

	// size==0 falls back to name-only — still a's file, never the folder.
	f, acct, ok = findCopyAcross(listings, "", "X", "X", 0)
	if !ok || acct != "acct-a" || f.ID != "a-copy" {
		t.Fatalf("name-only match = %s in %s ok=%v", f.ID, acct, ok)
	}

	// A remembered file id is searched in every account's root and Pack.
	f, acct, ok = findCopyAcross(listings, "b-root", "X", "X", 0)
	if !ok || acct != "acct-b" || f.ID != "b-root" {
		t.Fatalf("id match = %s in %s ok=%v", f.ID, acct, ok)
	}

	// A folder is never a copy, however well the name fits.
	folders := []packListing{
		{AccountID: "acct-c", Pack: []pikpak.File{{ID: "c-dir", Kind: "drive#folder", Name: "X"}}},
	}
	if _, _, ok := findCopyAcross(folders, "", "X", "X", 0); ok {
		t.Fatal("a folder matched by name")
	}
	if _, _, ok := findCopyAcross(folders, "", "X", "X", 100); ok {
		t.Fatal("a folder matched by name and size")
	}

	// The size must be exact: a same-named file of another size is not a copy.
	wrongSize := []packListing{
		{AccountID: "acct-d", Pack: []pikpak.File{{ID: "d-copy", Kind: "drive#file", Name: "X", Size: 200}}},
	}
	if _, _, ok := findCopyAcross(wrongSize, "", "X", "X", 100); ok {
		t.Fatal("matched a file of a different size")
	}
	if _, _, ok := findCopyAcross(listings, "", "missing", "missing", 0); ok {
		t.Fatal("matched a file that does not exist")
	}
}

// TestAnnotateUsesAnyAccount pins the restored badge against the whole set of
// bound accounts: the default account has no copy, a later one does, and the
// badge must still light up.
func TestAnnotateUsesAnyAccount(t *testing.T) {
	var mu sync.Mutex
	roots := map[string][]map[string]any{
		"at-acct-a": {},
		"at-acct-b": {{"id": "pack-b", "name": packFromSharedName, "kind": "drive#folder"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/share":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"share_status":    "OK",
				"pass_code_token": "tok-1",
				"title":           "资料",
				"files": []map[string]any{
					{"id": "sf2", "name": "movie.mkv", "kind": "drive#file", "size": "100"},
				},
			})
		case "/drive/v1/files":
			items := []map[string]any{{"id": "copy-1", "name": "movie.mkv", "kind": "drive#file", "size": "100"}}
			if r.URL.Query().Get("parent_id") == "" {
				mu.Lock()
				items = roots[token]
				mu.Unlock()
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"files": items, "next_page_token": ""})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/share?url=VOabc123&pass_code=7f3a", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("open status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["restored"] != true {
		t.Fatalf("restored = %v, want the copy on the second account", first["restored"])
	}
}
