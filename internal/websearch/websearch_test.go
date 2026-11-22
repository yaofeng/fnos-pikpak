package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// organicServer answers every search with the organic results it was built
// with and records the query and key it saw.
func organicServer(t *testing.T, results []map[string]any) (*httptest.Server, *string, *string) {
	t.Helper()
	var gotQuery, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body struct {
			Q string `json:"q"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotQuery = body.Q
		gotKey = r.Header.Get("X-API-KEY")
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": results})
	}))
	t.Cleanup(srv.Close)
	return srv, &gotQuery, &gotKey
}

func TestSearchAddsTheSiteFilterAndReturnsResults(t *testing.T) {
	srv, gotQuery, gotKey := organicServer(t, []map[string]any{
		{"title": "大模型合集", "link": "https://mypikpak.com/s/VN1", "snippet": "分享的大模型资料"},
		{"title": "第二份", "link": "https://www.mypikpak.com/s/VN2", "snippet": "更多资料"},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	got, err := c.Search(context.Background(), "key-123", "  大模型 ")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// The site filter is the server's business: the person types only the
	// keyword, and a pasted "site:" prefix never doubles up.
	if *gotQuery != "site:mypikpak.com 大模型" {
		t.Fatalf("query = %q", *gotQuery)
	}
	if *gotKey != "key-123" {
		t.Fatalf("key header = %q", *gotKey)
	}
	if len(got) != 2 {
		t.Fatalf("results = %+v", got)
	}
	if got[0].Title != "大模型合集" || got[0].URL != "https://mypikpak.com/s/VN1" || got[0].Snippet != "分享的大模型资料" {
		t.Fatalf("result[0] = %+v", got[0])
	}
}

func TestSearchStripsASiteFilterThePersonPasted(t *testing.T) {
	srv, gotQuery, _ := organicServer(t, nil)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := c.Search(context.Background(), "key-123", "site://mypikpak.com/ 大模型"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if *gotQuery != "site:mypikpak.com 大模型" {
		t.Fatalf("query = %q", *gotQuery)
	}
}

func TestSearchDropsResultsOutsidePikPak(t *testing.T) {
	srv, _, _ := organicServer(t, []map[string]any{
		{"title": "知乎", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
		{"title": "站内", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
		// A lookalike host must not pass for the site.
		{"title": "假冒", "link": "https://notmypikpak.com/s/VN1", "snippet": "相关"},
	})
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	got, err := c.Search(context.Background(), "key-123", "大模型")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://mypikpak.com/s/VN1" {
		t.Fatalf("results = %+v", got)
	}
}

func TestSearchRefusesAnEmptyKeywordWithoutCallingUpstream(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := c.Search(context.Background(), "key-123", "  "); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("upstream calls = %d", calls)
	}
}

func TestSearchReportsAnUpstreamFailureWithoutLeakingTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}

	_, err := c.Search(context.Background(), "key-123", "大模型")
	if err == nil {
		t.Fatal("a rejected request looked like an empty result")
	}
	if strings.Contains(err.Error(), "key-123") {
		t.Fatalf("the API key reached the error text: %v", err)
	}
}
