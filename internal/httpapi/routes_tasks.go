package httpapi

import (
	"net/http"
	"strings"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

func registerTaskRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		clients, ok := taskScopeClients(w, r, deps)
		if !ok {
			return
		}
		phase := strings.TrimSpace(r.URL.Query().Get("phase"))
		tokens, err := decodePageToken(r.URL.Query().Get("page_token"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_page_token", "page_token 不合法")
			return
		}
		pages, nexts, failures := fetchTaskPages(r.Context(), clients, tokens)
		// Accounts are concatenated in bind order — no cross-account sort —
		// and the phase filter applies to the merged slice exactly as it did
		// to the single page before.
		items := make([]pikpak.Task, 0, 16)
		for i, nc := range clients {
			if failures[i] != nil {
				continue
			}
			for _, task := range stampTasks(pages[i], nc) {
				if !task.IsOffline() {
					continue
				}
				if phase == "" || task.Phase == phase {
					items = append(items, task)
				}
			}
		}
		resp := tasksResponse{
			Items:         items,
			NextPageToken: nextPageToken(clients, nexts, failures),
		}
		if len(failures) > 0 {
			resp.AccountErrors = accountErrorList(clients, failures)
		}
		writeJSON(w, http.StatusOK, resp)
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
		// A named account pins the create to it. The rotation exists to spread
		// the daily quota across accounts, which only makes sense when the
		// page scope is "all": filtered to one account, landing the task on
		// another would contradict the page's own dropdown. So the pinned
		// branch skips rotatingAccounts, and an account-level refusal (quota,
		// rate limit, outage) surfaces as that account's own error the way a
		// single-account create has always reported it.
		if param := r.URL.Query().Get("account"); param != "" && param != "all" {
			client, acct, ok := accountClient(w, r, deps)
			if !ok {
				return
			}
			task, err := client.CreateOffline(r.Context(), link, strings.TrimSpace(body.ParentID))
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			resp := map[string]any{"task": task}
			if acct.ID != "" {
				resp["account_id"] = acct.ID
				resp["account_name"] = acct.DisplayName
			}
			writeJSON(w, http.StatusCreated, resp)
			return
		}
		clients, ok := rotatingAccounts(w, r, deps)
		if !ok {
			return
		}
		// The task goes to the first account that accepts it, tried in random
		// order so the load spreads. A failure that is the account's own
		// (quota, rate limit, outage) rotates to the next; a refusal that
		// travels with the link would fail on every account the same way, so
		// it stops the loop with the usual error mapping.
		type refusal struct{ name, reason string }
		var refused []refusal
		for _, nc := range clients {
			task, err := nc.C.CreateOffline(r.Context(), link, strings.TrimSpace(body.ParentID))
			if err == nil {
				resp := map[string]any{"task": task}
				if nc.Account.ID != "" {
					resp["account_id"] = nc.Account.ID
					resp["account_name"] = nc.Account.DisplayName
				}
				writeJSON(w, http.StatusCreated, resp)
				return
			}
			if !isAccountLevelErr(err) {
				writeUpstreamError(w, err)
				return
			}
			refused = append(refused, refusal{
				name:   accountLabel(nc.Account),
				reason: strings.TrimSpace(probe.Redact(err.Error())),
			})
		}
		parts := make([]string, len(refused))
		for i, rf := range refused {
			parts[i] = rf.name + "：" + rf.reason
		}
		writeError(w, http.StatusTooManyRequests, "all_accounts_failed", strings.Join(parts, "；"))
	})

	mux.HandleFunc("POST /api/tasks/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		client, _, ok := taskAccount(w, r, deps)
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
		client, _, ok := taskAccount(w, r, deps)
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
		clients, ok := taskScopeClients(w, r, deps)
		if !ok {
			return
		}
		// Finished tasks of every account in scope go; the counts add up
		// across accounts. A named ?account= shrinks the scope to that one
		// account, absent or "all" clears every active account as before.
		deleted := 0
		for _, nc := range clients {
			n, err := nc.C.ClearFinishedTasks(r.Context())
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			deleted += n
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
	})
}

// taskScopeClients resolves the ?account= scope the offline-task routes
// share. A concrete account id pins the slice to that one account — the
// offline-task page's account dropdown is the scope of every operation it
// drives (list, create, clear) — while absent or "all" keeps every active
// account merged. On failure the response is already written and ok is false.
func taskScopeClients(w http.ResponseWriter, r *http.Request, deps Deps) ([]namedClient, bool) {
	if param := r.URL.Query().Get("account"); param != "" && param != "all" {
		client, acct, ok := accountClient(w, r, deps)
		if !ok {
			return nil, false
		}
		return []namedClient{{Account: acct, C: client}}, true
	}
	return allAccountClients(w, r, deps)
}

// taskAccount resolves the account a task row names. Merged listings stamp
// every row with its account_id, so the front end always carries one; a bare
// call has no way to guess which account owns the task and is refused before
// touching any upstream.
func taskAccount(w http.ResponseWriter, r *http.Request, deps Deps) (*pikpak.Client, store.PikPakAccount, bool) {
	if r.URL.Query().Get("account") == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "请指定任务所属账号")
		return nil, store.PikPakAccount{}, false
	}
	return accountClient(w, r, deps)
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
