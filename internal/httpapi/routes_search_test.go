package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"fnos-pikpak/internal/websearch"
)

type fakeSearcher struct {
	query string
	site  string
	items []websearch.Result
	err   error
}

func (f *fakeSearcher) Search(_ context.Context, query, site string) ([]websearch.Result, error) {
	f.query, f.site = query, site
	return f.items, f.err
}

func TestWebSearchPassesQueryAndSiteToTheEngine(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "")
	search := &fakeSearcher{items: []websearch.Result{{Title: "结果一", URL: "https://example.com/a", Snippet: "说明"}}}
	deps.Search = search
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=电影%20下载&site=example.com", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if search.query != "电影 下载" || search.site != "example.com" {
		t.Fatalf("searcher got %q / %q", search.query, search.site)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	first, _ := items[0].(map[string]any)
	if first["title"] != "结果一" || first["url"] != "https://example.com/a" {
		t.Fatalf("item = %v", first)
	}
}

func TestWebSearchNeedsAQuery(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "")
	deps.Search = &fakeSearcher{}
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=%20%20", "1000")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	errBody, _ := body["error"].(map[string]any)
	if errBody["code"] != "empty_query" {
		t.Fatalf("code = %v", errBody["code"])
	}
}

func TestWebSearchReportsAnEngineFailure(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "")
	deps.Search = &fakeSearcher{err: errors.New("engine down")}
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=test", "1000")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}
