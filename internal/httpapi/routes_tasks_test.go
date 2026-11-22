package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	mux.HandleFunc("POST /drive/v1/tasks/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
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
	bindTestUser(t, st, 1000)
	env.deps = deps
	return env
}

func TestTasksListFiltersPhaseAndForwardsToken(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/tasks?phase=PHASE_TYPE_RUNNING&page_token=t0", "1000")
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
	if body["next_page_token"] != "n1" {
		t.Fatalf("token = %v", body["next_page_token"])
	}
	if env.listQuery["page_token"] != "t0" {
		t.Fatalf("upstream query = %v", env.listQuery)
	}
	// PikPak rejects phase filters with invalid_argument, so the handler must
	// filter the page itself rather than forward one.
	if _, ok := env.listQuery["filters"]; ok {
		t.Fatalf("filters forwarded upstream: %v", env.listQuery)
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
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "daily_limit_reached" {
		t.Fatalf("error = %v", body)
	}
}

func TestRetryTask(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/tasks/t1/retry", "1000", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestDeleteTasksForwardsIDsAndDeleteFiles(t *testing.T) {
	env := newTasksEnv(t, false)
	h := NewMux(env.deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/tasks?ids=t1,t2&delete_files=true", nil)
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
