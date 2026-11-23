package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fnos-pikpak/internal/websearch"
)

// serperStub answers like the search provider and records what it was asked.
func serperStub(t *testing.T, organic []map[string]any) (*httptest.Server, *string, *string, *int) {
	t.Helper()
	var gotQuery, gotKey string
	gotPage := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Q    string `json:"q"`
			Page int    `json:"page"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// The first query is the one under test; the page is filled from the
		// following share hosts when the first one answers with little.
		if gotQuery == "" {
			gotQuery = body.Q
			gotKey = r.Header.Get("X-API-KEY")
			gotPage = body.Page
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": organic})
	}))
	t.Cleanup(srv.Close)
	return srv, &gotQuery, &gotKey, &gotPage
}

func withSearchKey(t *testing.T, deps Deps, key string) {
	t.Helper()
	if err := deps.Store.SetSetting(context.Background(), "search_api_key", key); err != nil {
		t.Fatalf("set key: %v", err)
	}
}

func TestWebSearchUsesTheStoredKeyAndPrefiltersBySite(t *testing.T) {
	srv, gotQuery, gotKey, gotPage := serperStub(t, []map[string]any{
		{"title": "大模型合集", "link": "https://mypikpak.com/s/VN1", "snippet": "资料"},
		{"title": "无关站点", "link": "https://example.com/x", "snippet": "不该出现"},
	})
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	deps.WebSearch = &websearch.Client{Endpoint: srv.URL, HTTP: srv.Client()}
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if *gotQuery != "site:mypikpak.com 大模型" {
		t.Fatalf("upstream query = %q", *gotQuery)
	}
	if *gotKey != "key-123" {
		t.Fatalf("upstream key = %q", *gotKey)
	}
	if *gotPage != 1 {
		t.Fatalf("default page = %d", *gotPage)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	item, _ := items[0].(map[string]any)
	if item["title"] != "大模型合集" || item["url"] != "https://mypikpak.com/s/VN1" {
		t.Fatalf("item = %v", item)
	}
	if strings.Contains(rec.Body.String(), "key-123") {
		t.Fatal("the API key reached the response")
	}
}

func TestWebSearchReportsTheRawCountBeforeFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Q string `json:"q"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		organic := []map[string]any{}
		if strings.Contains(body.Q, "site:mypikpak.com ") {
			organic = []map[string]any{
				{"title": "分享", "link": "https://mypikpak.com/s/VN1", "snippet": "资料"},
				{"title": "论坛", "link": "https://www.zhihu.com/question/1", "snippet": "无关"},
				{"title": "博客", "link": "https://blog.example.com/p/1", "snippet": "无关"},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"organic": organic})
	}))
	t.Cleanup(srv.Close)
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	deps.WebSearch = &websearch.Client{Endpoint: srv.URL, HTTP: srv.Client()}
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	// Three organic entries came back; one was a share page. The page shows
	// both numbers so a short list can explain itself.
	if body["raw"] != float64(3) {
		t.Fatalf("raw = %v", body["raw"])
	}
}

// The scope setting decides whether pages that are not shares are listed.
// Share pages are always marked, so a mixed list can tell them apart.
func TestWebSearchHonoursTheSearchScopeSetting(t *testing.T) {
	srv, _, _, _ := serperStub(t, []map[string]any{
		{"title": "首页", "link": "https://mypikpak.com/", "snippet": "站内页面"},
		{"title": "分享", "link": "https://mypikpak.com/s/VN1", "snippet": "相关"},
	})
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	deps.WebSearch = &websearch.Client{Endpoint: srv.URL, HTTP: srv.Client()}
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	// The default keeps only the page the app can open.
	rec, body := getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 || body["scope"] != "share" {
		t.Fatalf("items = %v scope = %v", items, body["scope"])
	}

	if err := deps.Store.SetUserSetting(context.Background(), 1000, "search_scope", "all"); err != nil {
		t.Fatalf("set scope: %v", err)
	}
	rec, body = getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ = body["items"].([]any)
	if len(items) != 2 || body["scope"] != "all" {
		t.Fatalf("items = %v scope = %v", items, body["scope"])
	}
	first, _ := items[0].(map[string]any)
	second, _ := items[1].(map[string]any)
	if first["share"] != false || second["share"] != true {
		t.Fatalf("share marks = %v / %v", first["share"], second["share"])
	}
}

func TestWebSearchPassesThePageThrough(t *testing.T) {
	srv, _, _, gotPage := serperStub(t, []map[string]any{
		{"title": "第二页", "link": "https://mypikpak.com/s/VN2", "snippet": "资料"},
	})
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	deps.WebSearch = &websearch.Client{Endpoint: srv.URL, HTTP: srv.Client()}
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=大模型&page=2", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if *gotPage != 2 {
		t.Fatalf("upstream page = %d", *gotPage)
	}
	if body["page"] != float64(2) {
		t.Fatalf("page = %v", body["page"])
	}
}

func TestWebSearchRejectsAPageOutOfRange(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	for _, raw := range []string{"0", "11", "x"} {
		rec, body := getJSON(t, h, "/api/web-search?q=大模型&page="+raw, "1000")
		if rec.Code != http.StatusBadRequest || errCode(body) != "invalid_page" {
			t.Fatalf("page=%s: status = %d body=%v", raw, rec.Code, body)
		}
	}
}

func TestWebSearchExplainsAMissingKey(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusBadRequest || errCode(body) != "search_key_missing" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestWebSearchNeedsAQuery(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=%20", "1000")
	if rec.Code != http.StatusBadRequest || errCode(body) != "empty_query" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestWebSearchReportsARejectedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"invalid key"}`)
	}))
	t.Cleanup(srv.Close)
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	withSearchKey(t, deps, "key-123")
	deps.WebSearch = &websearch.Client{Endpoint: srv.URL, HTTP: srv.Client()}
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/web-search?q=大模型", "1000")
	if rec.Code != http.StatusBadGateway || errCode(body) != "search_key_rejected" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}
