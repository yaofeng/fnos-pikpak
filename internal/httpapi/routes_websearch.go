package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/websearch"
)

const (
	webSearchTimeout  = 20 * time.Second
	maxWebSearchQuery = 100
)

// registerWebSearchRoutes exposes 全网搜索. The query is the person's keyword
// only: the site: filter is added by the websearch package, and the provider
// key is read from the global settings table, never from the request.
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

		client := deps.WebSearch
		if client == nil {
			client = websearch.New()
		}
		ctx, cancel := context.WithTimeout(r.Context(), webSearchTimeout)
		defer cancel()
		ans, err := client.Search(ctx, apiKey, query, page)
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
		writeJSON(w, http.StatusOK, map[string]any{"items": ans.Results, "query": query, "page": page, "raw": ans.Raw})
	})
}
