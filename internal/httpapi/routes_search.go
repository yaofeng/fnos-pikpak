package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"fnos-pikpak/internal/pikpak"
)

const (
	// A search reads every folder because PikPak has no search endpoint, so it
	// gets a wall-clock budget and a folder/hit cap rather than running until
	// the drive ends.
	searchTimeout  = 20 * time.Second
	searchFolders  = 2000
	searchHitLimit = 200
	maxSearchQuery = 100
)

func registerSearchRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/files/search", func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, http.StatusBadRequest, "empty_query", "请输入搜索关键词")
			return
		}
		if len([]rune(query)) > maxSearchQuery {
			writeError(w, http.StatusBadRequest, "query_too_long", "关键词太长，请缩短后重试")
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), searchTimeout)
		defer cancel()
		hits, scanned, truncated, err := client.SearchFiles(ctx, query, pikpak.SearchLimits{
			MaxFolders: searchFolders,
			MaxHits:    searchHitLimit,
		})
		if err != nil {
			// A timeout returns what was found so far: a partial answer beats a
			// spinner on a large drive.
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				writeUpstreamError(w, err)
				return
			}
			truncated = true
		}

		items := make([]map[string]any, 0, len(hits))
		for _, hit := range hits {
			items = append(items, map[string]any{
				"id":            hit.File.ID,
				"parent_id":     hit.File.ParentID,
				"name":          hit.File.Name,
				"kind":          hit.File.Kind,
				"size":          hit.File.Size,
				"phase":         hit.File.Phase,
				"category":      hit.File.Category,
				"mime_type":     hit.File.MimeType,
				"modified_time": hit.File.ModifiedTime,
				"path":          hit.Path,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":     items,
			"scanned":   scanned,
			"truncated": truncated,
		})
	})
}
