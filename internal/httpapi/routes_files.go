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

type filesResponse struct {
	Items         []pikpak.File `json:"items"`
	NextPageToken string        `json:"next_page_token"`
	Filters       string        `json:"filters,omitempty"`
	QLocal        bool          `json:"q_local,omitempty"`
}

func registerFileRoutes(mux *http.ServeMux, deps Deps) {
	registerFileOperationRoutes(mux, deps)

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

		filters := ""
		if kind := r.URL.Query().Get("kind"); kind != "" {
			raw, err := json.Marshal(map[string]any{"kind": map[string]string{"eq": kind}})
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_kind", "kind 不合法")
				return
			}
			filters = string(raw)
		}

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
