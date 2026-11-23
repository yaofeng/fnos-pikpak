package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/websearch"
)

const (
	// The search asks up to four hosts one after another; nothing is resolved
	// while it runs.
	webSearchTimeout  = 20 * time.Second
	maxWebSearchQuery = 100
	// One click may wait this long for a wrapper to give up its address.
	webSearchResolveTimeout = 8 * time.Second
	// A provider's click redirect is a short URL; anything longer is not one.
	maxWebSearchLink = 4096
)

// webSearchClient is the provider client a handler should use, production by
// default.
func webSearchClient(deps Deps) *websearch.Client {
	if deps.WebSearch != nil {
		return deps.WebSearch
	}
	return websearch.New()
}

// registerWebSearchRoutes exposes 全网搜索. The query is the person's keyword
// only: the site: filter is added by the websearch package, and the provider
// key is read from the settings table, never from the request.
func registerWebSearchRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/web-search", func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, http.StatusBadRequest, "empty_query", "请输入搜索关键词")
			return
		}
		if len([]rune(query)) > maxWebSearchQuery {
			writeError(w, http.StatusBadRequest, "query_too_long", "关键词太长，请缩短后重试")
			return
		}
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > websearch.MaxPage {
				writeError(w, http.StatusBadRequest, "invalid_page", "页码超出范围（1–10）")
				return
			}
			page = n
		}

		apiKey, err := deps.Store.GetSetting(r.Context(), "search_api_key")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal", "读取搜索设置失败")
			return
		}
		if apiKey == "" {
			writeError(w, http.StatusBadRequest, "search_key_missing", "全网搜索需要先在参数设置里填写搜索 API Key")
			return
		}

		client := webSearchClient(deps)
		// The scope is a per-user preference: 只显示分享页 by default, or
		// every page on the PikPak hosts with the shares marked.
		id, _ := fnos.IdentityFrom(r.Context())
		scope := websearch.ScopeShare
		if v, err := deps.Store.GetUserSetting(r.Context(), id.UID, "search_scope"); err == nil && v == string(websearch.ScopeSite) {
			scope = websearch.ScopeSite
		}
		ctx, cancel := context.WithTimeout(r.Context(), webSearchTimeout)
		defer cancel()
		ans, err := client.Search(ctx, apiKey, query, page, scope)
		if err != nil {
			var upstream *websearch.UpstreamError
			if errors.As(err, &upstream) && (upstream.Status == http.StatusUnauthorized || upstream.Status == http.StatusForbidden) {
				writeError(w, http.StatusBadGateway, "search_key_rejected", "搜索 API Key 无效，请在参数设置里检查")
				return
			}
			if errors.Is(err, websearch.ErrEmptyQuery) {
				writeError(w, http.StatusBadRequest, "empty_query", "请输入搜索关键词")
				return
			}
			writeError(w, http.StatusBadGateway, "search_failed", "搜索服务暂时不可用，请稍后重试")
			return
		}
		// raw is what the provider returned before results outside the share
		// hosts were dropped; the page says so when the list is much shorter.
		writeJSON(w, http.StatusOK, map[string]any{
			"items": ans.Results, "query": query, "page": page, "raw": ans.Raw, "scope": string(scope),
		})
	})

	// GET /api/web-search/resolve is the click-time half of the search: a
	// result the provider wrapped in its own click redirect is listed as it
	// is, and this says what it leads to when someone opens it. The key is
	// optional here — Google's redirect can answer without it — so a missing
	// key does not block opening a result.
	mux.HandleFunc("GET /api/web-search/resolve", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.URL.Query().Get("url"))
		u, perr := url.Parse(raw)
		if perr != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || len(raw) > maxWebSearchLink {
			writeError(w, http.StatusBadRequest, "bad_link", "链接地址无效")
			return
		}
		apiKey, err := deps.Store.GetSetting(r.Context(), "search_api_key")
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal", "读取搜索设置失败")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), webSearchResolveTimeout)
		defer cancel()
		dest, err := webSearchClient(deps).ResolveLink(ctx, apiKey, raw)
		if err != nil {
			if errors.Is(err, websearch.ErrNotWrapped) {
				// Nothing to unwrap: the address is already the page.
				writeJSON(w, http.StatusOK, map[string]any{"url": raw})
				return
			}
			writeError(w, http.StatusBadGateway, "link_unresolved", "链接解析失败，稍后可直接打开搜索跳转地址")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"url": dest})
	})
}
