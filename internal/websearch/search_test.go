package websearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchScopesTheQueryToTheSiteAndParsesTheRSS(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "rss" {
			t.Errorf("format = %s", r.URL.Query().Get("format"))
		}
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0"><channel><title>必应：test</title>
<item><title>资源站 - 下载</title><link>https://example.com/a</link><description>一条说明</description></item>
<item><title>第二个结果</title><link>https://example.org/b</link><description>另一条</description></item>
</channel></rss>`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	items, err := c.Search(context.Background(), "电影 下载", "example.com")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotQuery != "site:example.com 电影 下载" {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(items) != 2 {
		t.Fatalf("items = %v", items)
	}
	if items[0].Title != "资源站 - 下载" || items[0].URL != "https://example.com/a" || items[0].Snippet != "一条说明" {
		t.Fatalf("first = %+v", items[0])
	}
}

func TestSearchWithoutASiteSendsThePlainQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	items, err := c.Search(context.Background(), " ubuntu ", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if gotQuery != "ubuntu" {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(items) != 0 {
		t.Fatalf("items = %v", items)
	}
}

func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	if _, err := NewClient().Search(context.Background(), "   ", ""); !errors.Is(err, ErrEmptyQuery) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchReportsAnUpstreamFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	if _, err := c.Search(context.Background(), "x", ""); err == nil {
		t.Fatal("a 500 answered as success")
	}
}

func TestSearchLimitsTheResultCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := `<item><title>a</title><link>https://example.com/a</link></item>`
		body := `<?xml version="1.0"?><rss><channel>`
		for i := 0; i < maxResults+5; i++ {
			body += items
		}
		body += `</channel></rss>`
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient()
	c.BaseURL = srv.URL
	got, err := c.Search(context.Background(), "x", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != maxResults {
		t.Fatalf("len = %d", len(got))
	}
}
