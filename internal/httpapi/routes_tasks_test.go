package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type tasksEnv struct {
	deps     Deps
	upstream *httptest.Server
	// createBody captures the POST /drive/v1/files body.
	createBody map[string]any
	// deleteQuery captures the DELETE /drive/v1/tasks query.
	deleteQuery map[string]string
	listQuery   map[string]string
	clearCalled bool
	// addTask appends a task to the upstream list.
	addTask func(map[string]any)
}

// newTasksEnv serves the offline-task upstream endpoints and records what the
// handlers sent. reply429 makes task creation answer with the daily quota
// error.
func newTasksEnv(t *testing.T, quota bool) *tasksEnv {
	t.Helper()
	env := &tasksEnv{}
	// The upstream keeps a task list so a delete actually shrinks it; a fixed
	// answer would make the clear loop spin until its round cap.
	live := []map[string]any{
		{"id": "t1", "phase": "PHASE_TYPE_RUNNING", "progress": 30, "file_name": "a.mkv", "file_size": "10"},
		{"id": "t2", "phase": "PHASE_TYPE_COMPLETE", "progress": 100, "file_name": "b.mkv", "file_size": "20"},
	}
	env.addTask = func(task map[string]any) { live = append(live, task) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /drive/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		env.listQuery = map[string]string{}
		for k := range r.URL.Query() {
			env.listQuery[k] = r.URL.Query().Get(k)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": live, "next_page_token": "n1"})
	})
	mux.HandleFunc("POST /drive/v1/tasks:clear", func(w http.ResponseWriter, r *http.Request) {
		env.clearCalled = true
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("POST /drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&env.createBody)
		if quota {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"file_task_quota_exceeded","error_code":11}`)
			return
		}
		_, _ = io.WriteString(w, `{"task":{"id":"t9","phase":"PHASE_TYPE_PENDING","file_name":"x"}}`)
	})
	mux.HandleFunc("POST /drive/v1/task", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("DELETE /drive/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		env.deleteQuery = map[string]string{}
		for k := range r.URL.Query() {
			env.deleteQuery[k] = r.URL.Query().Get(k)
		}
		drop := map[string]bool{}
		for _, id := range strings.Split(r.URL.Query().Get("task_ids"), ",") {
			drop[id] = true
		}
		kept := live[:0]
		for _, task := range live {
			if !drop[task["id"].(string)] {
				kept = append(kept, task)
			}
		}
		live = kept
		_, _ = io.WriteString(w, `{}`)
	})

	env.upstream = httptest.NewServer(mux)
	t.Cleanup(env.upstream.Close)
	deps, st := newAuthTestDeps(t, env.upstream.URL)
	// The merged task routes iterate real account rows, so the env binds one
	// named account rather than a bare credential.
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	env.deps = deps
	return env
}

func TestTasksListFiltersPhaseAndForwardsToken(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/tasks?phase=PHASE_TYPE_RUNNING", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("phase filter returned %v", items)
	}
	first, _ := items[0].(map[string]any)
	if first["id"] != "t1" || first["progress"] != float64(30) {
		t.Fatalf("item = %v", first)
	}
	// Every merged row says which account it came from.
	if first["account_id"] != "acct-a" || first["account_name"] != "账号acct-a" {
		t.Fatalf("stamp = %v/%v", first["account_id"], first["account_name"])
	}
	// The next-page token is the handler's own envelope; the upstream token
	// travels inside it and is forwarded on the following request.
	token, _ := body["next_page_token"].(string)
	if token == "" || token == "n1" {
		t.Fatalf("next_page_token = %v, want the merged envelope", body["next_page_token"])
	}
	rec, body = getJSON(t, h, "/api/tasks?phase=PHASE_TYPE_RUNNING&page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%v", rec.Code, body)
	}
	if env.listQuery["page_token"] != "n1" {
		t.Fatalf("upstream query = %v", env.listQuery)
	}
	// PikPak rejects phase filters with invalid_argument, so the handler must
	// filter the page itself rather than forward one.
	if _, ok := env.listQuery["filters"]; ok {
		t.Fatalf("filters forwarded upstream: %v", env.listQuery)
	}
}

func TestTasksListHidesPikPakInternalTasks(t *testing.T) {
	env := newTasksEnv(t, false)
	// PikPak lists its own maintenance tasks next to the downloads: deleting
	// files queues a task of type "event-deletion". The offline-task page
	// must not show them.
	env.addTask(map[string]any{"id": "e1", "type": "event-deletion", "name": "event-deletion", "phase": "PHASE_TYPE_COMPLETE", "progress": 100})
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/tasks", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	var ids []string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		id, _ := item["id"].(string)
		ids = append(ids, id)
	}
	for _, id := range ids {
		if id == "e1" {
			t.Fatalf("internal task reached the offline list: %v", ids)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("offline tasks missing: %v", ids)
	}
}

func TestCreateTaskUsesDownloadFolderType(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks", "1000",
		`{"link":"magnet:?xt=urn:btih:abc","parent_id":"p1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	task, _ := body["task"].(map[string]any)
	if task["id"] != "t9" {
		t.Fatalf("task = %v", body)
	}
	// The response names the account that took the task.
	if body["account_id"] != "acct-a" || body["account_name"] != "账号acct-a" {
		t.Fatalf("stamp = %v/%v", body["account_id"], body["account_name"])
	}
	if env.createBody["folder_type"] != "DOWNLOAD" || env.createBody["upload_type"] != "UPLOAD_TYPE_URL" {
		t.Fatalf("body = %v", env.createBody)
	}
	if env.createBody["parent_id"] != "p1" {
		t.Fatalf("parent_id = %v", env.createBody["parent_id"])
	}
	link, _ := env.createBody["url"].(map[string]any)
	if link["url"] != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("url = %v", env.createBody["url"])
	}
}

func TestCreateTaskRequiresLink(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks", "1000", `{"link":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestCreateTaskDailyLimitIs429(t *testing.T) {
	env := newTasksEnv(t, true)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks", "1000", `{"link":"https://example.com/a.bin"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	// With every account exhausted the refusal is the rotation's, and the
	// message names the account that refused.
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "all_accounts_failed" {
		t.Fatalf("error = %v", body)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "账号acct-a") {
		t.Fatalf("message = %v, want the account name", errObj["message"])
	}
}

func TestCreateTaskRotatesAccounts(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		calls[token]++
		mu.Unlock()
		if token == "at-acct-a" {
			// The first account is out of offline quota: an account-level
			// refusal the rotation must move past.
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"file_task_quota_exceeded","error_code":11}`)
			return
		}
		_, _ = io.WriteString(w, `{"task":{"id":"t9","phase":"PHASE_TYPE_PENDING","file_name":"x"}}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	snapshot := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]int, len(calls))
		for k, v := range calls {
			out[k] = v
		}
		return out
	}

	// The shuffle order is random, so the request repeats until the failing
	// account has actually been tried; every repetition must serve 201 from
	// the healthy account with exactly one CreateOffline there and at most
	// one on the failing one.
	rotated := false
	for i := 0; i < 30 && !rotated; i++ {
		before := snapshot()
		rec, body := postJSON(t, h, "/api/tasks", "1000", `{"link":"https://example.com/a.bin"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%v", rec.Code, body)
		}
		if body["account_id"] != "acct-b" {
			t.Fatalf("account_id = %v, want the healthy acct-b", body["account_id"])
		}
		after := snapshot()
		if d := after["at-acct-b"] - before["at-acct-b"]; d != 1 {
			t.Fatalf("healthy upstream calls per request = %d, want 1", d)
		}
		d := after["at-acct-a"] - before["at-acct-a"]
		if d > 1 {
			t.Fatalf("failing upstream calls per request = %d, want at most one", d)
		}
		rotated = d == 1
	}
	if !rotated {
		t.Fatal("the failing account was never tried; rotation never happened")
	}
}

func TestCreateTaskAllAccountsFail(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		calls[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"file_task_quota_exceeded","error_code":11}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/tasks", "1000", `{"link":"https://example.com/a.bin"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "all_accounts_failed" {
		t.Fatalf("error = %v", body)
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "账号acct-a") || !strings.Contains(msg, "账号acct-b") {
		t.Fatalf("message = %q, want every account named", msg)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["Bearer at-acct-a"] != 1 || calls["Bearer at-acct-b"] != 1 {
		t.Fatalf("calls = %v, want one try per account", calls)
	}
}

func TestTaskMutationsRequireAccount(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	// A merged-listing row always carries its account, so a bare mutation has
	// no account to act as and is refused before touching any upstream.
	rec, body := postJSON(t, h, "/api/tasks/t1/retry", "1000", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("retry status = %d body=%v", rec.Code, body)
	}
	if errCode(body) != "bad_request" {
		t.Fatalf("retry error = %v, want bad_request", body["error"])
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks?ids=t1", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("delete status = %d body=%s", rec2.Code, rec2.Body)
	}
	if errCode(body) != "bad_request" {
		t.Fatalf("delete error = %v, want bad_request", body["error"])
	}
	if env.deleteQuery != nil {
		t.Fatalf("the delete reached the upstream without an account: %v", env.deleteQuery)
	}

	// Naming the account sends the mutation to that account's upstream.
	req = httptest.NewRequest(http.MethodDelete, "/api/tasks?ids=t1,t2&delete_files=true&account=acct-a", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec2 = httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete with account status = %d body=%s", rec2.Code, rec2.Body)
	}
	if env.deleteQuery["task_ids"] != "t1,t2" {
		t.Fatalf("task_ids = %q", env.deleteQuery["task_ids"])
	}
	if rec, body := postJSON(t, h, "/api/tasks/t1/retry?account=acct-a", "1000", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("retry with account status = %d body=%v", rec.Code, body)
	}
}

func TestRetryTask(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks/t1/retry?account=acct-a", "1000", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestDeleteTasksForwardsIDsAndDeleteFiles(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks?ids=t1,t2&delete_files=true&account=acct-a", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if env.deleteQuery["task_ids"] != "t1,t2" {
		t.Fatalf("task_ids = %q", env.deleteQuery["task_ids"])
	}
	if !strings.EqualFold(env.deleteQuery["delete_files"], "true") {
		t.Fatalf("delete_files = %q", env.deleteQuery["delete_files"])
	}
}

func TestClearTasksDeletesOnlyFinishedOnes(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks/clear", "1000", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["deleted"] != float64(1) {
		t.Fatalf("deleted = %v", body["deleted"])
	}
	if env.deleteQuery["task_ids"] != "t2" {
		t.Fatalf("task_ids = %q", env.deleteQuery["task_ids"])
	}
	// The running task is still there.
	_, after := getJSON(t, h, "/api/tasks", "1000")
	items, _ := after["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("tasks after clear = %v", items)
	}
	if first, _ := items[0].(map[string]any); first["id"] != "t1" {
		t.Fatalf("surviving task = %v", first)
	}
}

func TestTasksRequireBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/tasks", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
