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
	Items []pikpak.File `json:"items"`
	// NextPageToken is omitted once empty: a merged listing ends by leaving
	// the field out (the recent/tasks envelopes do the same), and the frontend
	// reads it with "|| ''" either way.
	NextPageToken string         `json:"next_page_token,omitempty"`
	Filters       string         `json:"filters,omitempty"`
	QLocal        bool           `json:"q_local,omitempty"`
	AccountErrors []accountError `json:"account_errors,omitempty"`
}

func registerFileRoutes(mux *http.ServeMux, deps Deps) {
	registerFileOperationRoutes(mux, deps)

	mux.HandleFunc("GET /api/recent", func(w http.ResponseWriter, r *http.Request) {
		clients, ok := allAccountClients(w, r, deps)
		if !ok {
			return
		}
		tokens, err := decodePageToken(r.URL.Query().Get("page_token"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_page_token", "page_token 不合法")
			return
		}
		pages, nexts, failures := fetchFilePages(r.Context(), clients, tokens)
		items := mergeFilePages(pages, clients)
		if items == nil {
			items = []pikpak.File{}
		}
		resp := recentResponse{
			Items:         items,
			NextPageToken: nextPageToken(clients, nexts, failures),
		}
		if len(failures) > 0 {
			resp.AccountErrors = accountErrorList(clients, failures)
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("GET /api/trash", func(w http.ResponseWriter, r *http.Request) {
		client, _, ok := accountClient(w, r, deps)
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
		client, _, ok := accountClient(w, r, deps)
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
		if r.URL.Query().Get("account") == "all" {
			handleMergedFiles(w, r, deps)
			return
		}
		client, _, ok := accountClient(w, r, deps)
		if !ok {
			return
		}

		limit, filters, ok := fileListParams(w, r)
		if !ok {
			return
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

// fileListParams parses the query GET /api/files shares across its
// single-account and merged branches: the page size (default 100, hard cap
// 200, the limit error copy identical in both) and the server-side filters
// (live files, optional kind). It writes the 400 itself and returns ok=false
// on a malformed limit.
func fileListParams(w http.ResponseWriter, r *http.Request) (limit int, filters string, ok bool) {
	limit = defaultFileLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_limit", "limit 必须是 0 到 200 的整数")
			return 0, "", false
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
		// json.Marshal of a static map cannot fail; the 400 is unreachable but
		// kept for shape parity with the single-account path it came from.
		writeError(w, http.StatusBadRequest, "bad_kind", "kind 不合法")
		return 0, "", false
	}
	return limit, string(rawFilters), true
}

// handleMergedFiles serves GET /api/files?account=all: every active account's
// page is fetched in parallel and merged by modified time, each row stamped
// with the account that served it. A failed account cannot blank the others'
// rows — it lands in account_errors instead — and the page token is the
// handler's own per-account envelope, reusing the recent/tasks protocol.
func handleMergedFiles(w http.ResponseWriter, r *http.Request, deps Deps) {
	clients, ok := allAccountClients(w, r, deps)
	if !ok {
		return
	}
	tokens, err := decodePageToken(r.URL.Query().Get("page_token"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_page_token", "page_token 不合法")
		return
	}
	limit, filters, ok := fileListParams(w, r)
	if !ok {
		return
	}

	// Every account starts at its root; with ?seg= name segments each
	// account's folder at that same path is resolved first (Task 3's walker,
	// cached per user/account/path) and those folders are what get listed.
	// An account whose resolution fails simply lacks the path: it is
	// silently absent — no rows, and no account_errors entry, because
	// absence is not a fault. Presetting its token to pageDone keeps the
	// listing from asking it and later pages from resurrecting it.
	segs := r.URL.Query()["seg"]
	parentIDs := make([]string, len(clients))
	if len(segs) > 0 {
		id, _ := fnos.IdentityFrom(r.Context())
		uid := strconv.Itoa(id.UID)
		for i, nc := range clients {
			parent, err := resolveFolderPath(r.Context(), uid, nc, segs, deps.PathCache)
			if err != nil {
				tokens[nc.Account.ID] = pageDone
				continue
			}
			parentIDs[i] = parent
		}
	}

	pages, nexts, failures := fetchListPages(r.Context(), clients, parentIDs, filters, limit, tokens)
	items := mergeFilePages(pages, clients)
	if items == nil {
		items = []pikpak.File{}
	}
	resp := filesResponse{
		Items:         items,
		NextPageToken: nextPageToken(clients, nexts, failures),
	}
	if len(failures) > 0 {
		resp.AccountErrors = accountErrorList(clients, failures)
	}
	if q := r.URL.Query().Get("q"); q != "" {
		// PikPak cannot filter by name (verified in M0), so the query is
		// applied to the loaded page only and the client is told so.
		resp.Items = filterByName(items, q)
		resp.QLocal = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeUpstreamError maps a PikPak failure onto the HTTP error envelope.
func writeUpstreamError(w http.ResponseWriter, err error) {
	// An expired grant is not an upstream fault: the user has to rebind, and
	// the client tells them so instead of showing a generic 502.
	if errors.Is(err, pikpak.ErrReauthRequired) {
		writeError(w, http.StatusUnauthorized, "pikpak_reauth_required", "PikPak 授权已失效，请重新绑定账号")
		return
	}
	// Code 11 is PikPak's daily offline-task quota and a plain upstream 429 is
	// rate limiting: both are expected refusals, not a broken upstream, so the
	// client gets 429 and can explain what to wait for.
	var apiErr *pikpak.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Code == 11 {
			writeError(w, http.StatusTooManyRequests, "daily_limit_reached", "离线任务每日额度已用完，请明天再试")
			return
		}
		if apiErr.Status == http.StatusTooManyRequests {
			writeError(w, http.StatusTooManyRequests, "too_many_requests", "PikPak 请求过于频繁，请稍后重试")
			return
		}
	}
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
