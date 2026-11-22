package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
)

const (
	defaultFileLimit = 100
	maxFileLimit     = 200
)

const trashFilters = `{"trashed":{"eq":true}}`

type filesResponse struct {
	Items         []pikpak.File `json:"items"`
	NextPageToken string        `json:"next_page_token"`
	Filters       string        `json:"filters,omitempty"`
	QLocal        bool          `json:"q_local,omitempty"`
}

func registerFileRoutes(mux *http.ServeMux, deps Deps) {
	registerFileOperationRoutes(mux, deps)

	mux.HandleFunc("GET /api/recent", func(w http.ResponseWriter, r *http.Request) {
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		page, err := client.Events(r.Context(), r.URL.Query().Get("page_token"))
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, filesResponse{
			Items:         nonNilFiles(page.Items),
			NextPageToken: page.NextPageToken,
		})
	})

	mux.HandleFunc("GET /api/trash", func(w http.ResponseWriter, r *http.Request) {
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		// The trash lives at the drive root; the filter, not a parent id,
		// selects it.
		page, err := client.ListFiles(r.Context(), "", pikpak.ListOptions{
			PageToken: r.URL.Query().Get("page_token"),
			Limit:     defaultFileLimit,
			Filters:   trashFilters,
		})
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, filesResponse{
			Items:         nonNilFiles(page.Items),
			NextPageToken: page.NextPageToken,
			Filters:       trashFilters,
		})
	})

	mux.HandleFunc("POST /api/trash/empty", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}
		taskID, err := client.EmptyTrash(r.Context())
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		resp, err := batchResult(r, client, taskID)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}

		limit := defaultFileLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "bad_limit", "limit 必须是 0 到 200 的整数")
				return
			}
			switch {
			case n > maxFileLimit:
				limit = maxFileLimit
			case n > 0:
				limit = n
			}
		}

		// Trashed files are excluded explicitly: PikPak's default listing
		// otherwise still returns items moved to the trash.
		query := map[string]any{"trashed": map[string]any{"eq": false}}
		if kind := r.URL.Query().Get("kind"); kind != "" {
			query["kind"] = map[string]string{"eq": kind}
		}
		rawFilters, err := json.Marshal(query)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_kind", "kind 不合法")
			return
		}
		filters := string(rawFilters)

		page, err := client.ListFiles(r.Context(), r.URL.Query().Get("parent_id"), pikpak.ListOptions{
			PageToken: r.URL.Query().Get("page_token"),
			Limit:     limit,
			Filters:   filters,
		})
		if err != nil {
			writeUpstreamError(w, err)
			return
		}

		resp := filesResponse{
			Items:         page.Items,
			NextPageToken: page.NextPageToken,
			Filters:       filters,
		}
		if q := r.URL.Query().Get("q"); q != "" {
			// PikPak cannot filter by name (verified in M0), so the query is
			// applied to the loaded page only and the client is told so.
			resp.Items = filterByName(page.Items, q)
			resp.QLocal = true
		}
		if resp.Items == nil {
			resp.Items = []pikpak.File{}
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

// boundClient returns the user's PikPak client, writing 401 when the account
// is not linked. A failed session load is a 500, not a silent empty list.
func boundClient(w http.ResponseWriter, r *http.Request, deps Deps) (*pikpak.Client, bool) {
	id, _ := fnos.IdentityFrom(r.Context())
	client, err := deps.Sessions.ClientFor(r.Context(), id.UID)
	if errors.Is(err, pikpak.ErrNotBound) {
		writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "无法加载 PikPak 会话")
		return nil, false
	}
	return client, true
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
}

func filterByName(items []pikpak.File, q string) []pikpak.File {
	needle := strings.ToLower(q)
	out := make([]pikpak.File, 0, len(items))
	for _, f := range items {
		if strings.Contains(strings.ToLower(f.Name), needle) {
			out = append(out, f)
		}
	}
	return out
}

func nonNilFiles(items []pikpak.File) []pikpak.File {
	if items == nil {
		return []pikpak.File{}
	}
	return items
}

// batchResult waits for a PikPak batch task and describes it for the client.
// A task still running at the deadline is reported as pending rather than as
// a success the caller cannot see yet.
func batchResult(r *http.Request, client *pikpak.Client, taskID string) (map[string]any, error) {
	resp := map[string]any{"ok": true, "pending": false}
	if taskID == "" {
		return resp, nil
	}
	task, done, err := client.WaitTaskSettled(r.Context(), taskID, batchWaitTimeout, taskPollInterval)
	if err != nil {
		return nil, err
	}
	resp["task_id"] = taskID
	resp["pending"] = !done
	if task.Phase != "" {
		resp["phase"] = task.Phase
	}
	if task.Phase == "PHASE_TYPE_ERROR" {
		resp["ok"] = false
	}
	return resp, nil
}
