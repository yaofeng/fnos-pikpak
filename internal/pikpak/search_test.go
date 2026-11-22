package pikpak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// searchTree serves a small drive: root holds folder A and one file; A holds
// folder B and a file; B holds one file.
func searchTree(t *testing.T) *Client {
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
		parent := r.URL.Query().Get("parent_id")
		_ = json.NewEncoder(w).Encode(map[string]any{"files": files[parent], "next_page_token": ""})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	return c
}

func TestSearchFilesWalksEveryFolderAndReportsThePath(t *testing.T) {
	c := searchTree(t)

	hits, scanned, truncated, err := c.SearchFiles(context.Background(), "movie", SearchLimits{MaxFolders: 100, MaxHits: 100})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	if scanned != 6 {
		t.Fatalf("scanned = %d", scanned)
	}
	byID := map[string]SearchHit{}
	for _, hit := range hits {
		byID[hit.File.ID] = hit
	}
	// The folder "Movies" matches too: the search covers names, not just files.
	if len(hits) != 4 {
		t.Fatalf("hits = %+v", hits)
	}
	if p := byID["a"].Path; len(p) != 0 {
		t.Fatalf("a path = %+v", p)
	}
	if _, ok := byID["f2"]; ok {
		t.Fatal("a name without the query matched")
	}
	if p := byID["f1"].Path; len(p) != 1 || p[0].Name != "Movies" || p[0].ID != "a" {
		t.Fatalf("f1 path = %+v", p)
	}
	if p := byID["f3"].Path; len(p) != 2 || p[1].Name != "Season 1" {
		t.Fatalf("f3 path = %+v", p)
	}
}

func TestSearchFilesMatchesFolderNamesToo(t *testing.T) {
	c := searchTree(t)
	hits, _, _, err := c.SearchFiles(context.Background(), "season", SearchLimits{MaxFolders: 100, MaxHits: 100})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if len(hits) != 1 || hits[0].File.ID != "b" || hits[0].File.Kind != "drive#folder" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchFilesStopsAtTheFolderLimit(t *testing.T) {
	c := searchTree(t)
	hits, _, truncated, err := c.SearchFiles(context.Background(), "movie", SearchLimits{MaxFolders: 1, MaxHits: 100})
	if err != nil {
		t.Fatalf("SearchFiles: %v", err)
	}
	if !truncated {
		t.Fatal("the walk ran past its folder limit")
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	for _, hit := range hits {
		if hit.File.Kind == "drive#file" && hit.File.ID != "f0" {
			t.Fatalf("hit from an unvisited folder: %+v", hit)
		}
	}
}

func TestSearchFilesRefusesAnEmptyQuery(t *testing.T) {
	c := searchTree(t)
	if _, _, _, err := c.SearchFiles(context.Background(), "  ", SearchLimits{MaxFolders: 10, MaxHits: 10}); err == nil {
		t.Fatal("empty query searched the whole drive")
	}
}
