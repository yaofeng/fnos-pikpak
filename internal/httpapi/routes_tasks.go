package httpapi

import (
	"net/http"
	"strings"

	"fnos-pikpak/internal/pikpak"
)

func registerTaskRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		phase := strings.TrimSpace(r.URL.Query().Get("phase"))
		page, err := client.ListTasks(r.Context(), r.URL.Query().Get("page_token"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		// PikPak answers any phase filter with invalid_argument, so the phase
		// is applied to the page that was loaded.
		items := make([]pikpak.Task, 0, len(page.Items))
		for _, task := range page.Items {
			if phase == "" || task.Phase == phase {
				items = append(items, task)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":           items,
			"next_page_token": page.NextPageToken,
		})
	})

	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Link     string `json:"link"`
			ParentID string `json:"parent_id"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		link := strings.TrimSpace(body.Link)
		if link == "" {
			writeError(w, http.StatusBadRequest, "empty_link", "请填写下载链接或磁力链接")
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		task, err := client.CreateOffline(r.Context(), link, strings.TrimSpace(body.ParentID))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"task": task})
	})

	mux.HandleFunc("POST /api/tasks/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if err := client.RetryTask(r.Context(), r.PathValue("id")); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("DELETE /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		ids := splitQueryIDs(r.URL.Query().Get("ids"))
		if len(ids) == 0 {
			writeError(w, http.StatusBadRequest, "bad_ids", "缺少任务 ID")
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		deleteFiles := r.URL.Query().Get("delete_files") == "true"
		if err := client.DeleteTasks(r.Context(), ids, deleteFiles); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("POST /api/tasks/clear", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		if err := client.ClearTasks(r.Context()); err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// splitQueryIDs parses a comma-separated id list from a query parameter,
// dropping blanks and duplicates.
func splitQueryIDs(raw string) []string {
	seen := map[string]bool{}
	ids := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
