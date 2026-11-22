package httpapi

import (
	"context"
	"net/http"
	"strings"

	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/websearch"
)

// Searcher runs the web search behind 全网搜索.
type Searcher interface {
	Search(ctx context.Context, query, site string) ([]websearch.Result, error)
}

const maxSearchQuery = 200

func registerSearchRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/web-search", func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, http.StatusBadRequest, "empty_query", "请输入搜索关键词")
			return
		}
		if len([]rune(query)) > maxSearchQuery {
			writeError(w, http.StatusBadRequest, "query_too_long", "关键词太长，请缩短后重试")
			return
		}
		items, err := deps.Search.Search(r.Context(), query, strings.TrimSpace(r.URL.Query().Get("site")))
		if err != nil {
			writeError(w, http.StatusBadGateway, "search_failed", probe.Redact(err.Error()))
			return
		}
		if items == nil {
			items = []websearch.Result{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})
}
