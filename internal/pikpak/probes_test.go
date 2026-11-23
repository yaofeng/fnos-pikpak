package pikpak

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOrderCandidatesTryEncodedValues(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, r.URL.Query().Get("order"))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"a.bin","kind":"drive#file","size":"1"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	results := c.OrderCandidates(context.Background())

	// The device rejected the four bare values with 400, so the probe has to
	// try the encodings the API might expect: JSON strings and the share-link
	// vocabulary.
	want := []string{`"name"`, `"-size"`, `"-modified_time"`, `"-created_time"`, "name_asc", "size_desc", "mod_desc"}
	mu.Lock()
	got := append([]string(nil), sent...)
	mu.Unlock()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("order %q was not tried; sent = %q", w, got)
		}
	}
	if len(results) != len(got) {
		t.Fatalf("results = %d, requests = %d", len(results), len(got))
	}
	for _, r := range results {
		if r.Status != 200 || r.FirstName != "a.bin" {
			t.Fatalf("result = %+v", r)
		}
	}
}

func TestSearchCandidatesDetectFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("filters") != "":
			// A real search: every name contains the query.
			_, _ = io.WriteString(w, `{"files":[{"name":"test-1.bin"},{"name":"test-2.bin"}]}`)
		case q.Get("q") != "":
			// The list endpoint ignores q and returns the folder listing.
			_, _ = io.WriteString(w, `{"files":[{"name":"movie.mkv"},{"name":"photo.jpg"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found"}`)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	results := c.SearchCandidates(context.Background(), "test")

	var filtered, unfiltered *SearchResult
	for i := range results {
		r := &results[i]
		if r.Status != 200 || !r.LooksLikeFileList {
			continue
		}
		if strings.Contains(r.BodyPrefix, "test-1.bin") {
			filtered = r
		} else if strings.Contains(r.BodyPrefix, "movie.mkv") {
			unfiltered = r
		}
	}
	if filtered == nil {
		t.Fatalf("no filtered candidate; results = %+v", results)
	}
	if !filtered.Matched {
		t.Fatalf("filtered candidate not marked matched: %+v", filtered)
	}
	if unfiltered == nil {
		t.Fatalf("no unfiltered candidate; results = %+v", results)
	}
	if unfiltered.Matched {
		t.Fatalf("unfiltered candidate marked matched: %+v", unfiltered)
	}
}

func TestSearchCandidatesTreatEmptyResultAsFiltered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" && r.URL.Query().Get("filters") == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	results := c.SearchCandidates(context.Background(), "fnos-pikpak-probe-no-such-name")

	sawEmpty := false
	for _, r := range results {
		if r.Status == 200 && r.LooksLikeFileList {
			sawEmpty = true
			if !r.Matched {
				t.Fatalf("empty result not marked matched: %+v", r)
			}
		}
	}
	if !sawEmpty {
		t.Fatalf("no candidate reached the empty list: %+v", results)
	}
}

func TestSearchFilterCandidateCarriesPhaseAndTrashed(t *testing.T) {
	var mu sync.Mutex
	var filters []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := r.URL.Query().Get("filters"); f != "" {
			mu.Lock()
			filters = append(filters, f)
			mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	c.SearchCandidates(context.Background(), "test")

	mu.Lock()
	defer mu.Unlock()
	for _, f := range filters {
		var parsed map[string]map[string]any
		if err := json.Unmarshal([]byte(f), &parsed); err != nil {
			t.Fatalf("filters %q: %v", f, err)
		}
		if _, ok := parsed["phase"]; !ok {
			t.Fatalf("filters %q lacks phase, the device rejected such filters", f)
		}
		if _, ok := parsed["trashed"]; !ok {
			t.Fatalf("filters %q lacks trashed", f)
		}
	}
}
