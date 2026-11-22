package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateOfflinePostsURLTask(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/drive/v1/files" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"task":{"id":"t1","type":"offline","phase":"PHASE_TYPE_PENDING",
			"file_id":"f1","file_name":"index.html","file_size":"1234"},"upload_type":"UPLOAD_TYPE_URL"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	task, err := c.CreateOffline(context.Background(), "https://example.com/index.html", "")
	if err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}
	if task.ID != "t1" || task.Phase != "PHASE_TYPE_PENDING" || task.FileSize != 1234 {
		t.Fatalf("task = %+v", task)
	}
	if gotBody["upload_type"] != "UPLOAD_TYPE_URL" || gotBody["folder_type"] != "DOWNLOAD" {
		t.Fatalf("body = %v", gotBody)
	}
	url, _ := gotBody["url"].(map[string]any)
	if url["url"] != "https://example.com/index.html" {
		t.Fatalf("url = %v", gotBody["url"])
	}
	if _, ok := gotBody["parent_id"]; ok {
		t.Fatalf("empty parent must be omitted: %v", gotBody)
	}
}

func TestCreateOfflineSendsParentWhenSet(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"task":{"id":"t1","phase":"PHASE_TYPE_PENDING"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	if _, err := c.CreateOffline(context.Background(), "https://example.com/x", "p1"); err != nil {
		t.Fatalf("CreateOffline: %v", err)
	}
	if gotBody["parent_id"] != "p1" {
		t.Fatalf("parent_id = %v", gotBody["parent_id"])
	}
}

func TestGetTaskParsesPhaseAndSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/tasks/t1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_COMPLETE","progress":100,
			"file_id":"f1","file_name":"a.bin","file_size":"20971520"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	task, err := c.GetTask(context.Background(), "t1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Phase != "PHASE_TYPE_COMPLETE" || task.FileSize != 20971520 || task.FileID != "f1" {
		t.Fatalf("task = %+v", task)
	}
}

func TestWaitTaskStopsOnErrorPhase(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&calls, 1) {
		case 1:
			_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_RUNNING","progress":10}`)
		default:
			_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_ERROR","message":"quota"}`)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	task, err := c.WaitTask(context.Background(), "t1", 5*time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask: %v", err)
	}
	if task.Phase != "PHASE_TYPE_ERROR" {
		t.Fatalf("task = %+v", task)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestWaitTaskTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_RUNNING"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	task, err := c.WaitTask(context.Background(), "t1", 60*time.Millisecond, 10*time.Millisecond)
	if err != ErrTaskTimeout {
		t.Fatalf("err = %v, want ErrTaskTimeout", err)
	}
	if task.Phase != "PHASE_TYPE_RUNNING" {
		t.Fatalf("last task = %+v", task)
	}
}

func TestDeleteTaskSendsQuery(t *testing.T) {
	var gotQuery string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	if err := c.DeleteTask(context.Background(), "t1", true); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s", gotMethod)
	}
	if !strings.Contains(gotQuery, "task_ids=t1") || !strings.Contains(gotQuery, "delete_files=true") {
		t.Fatalf("query = %s", gotQuery)
	}
}

func TestWalkPagesDetectsLoop(t *testing.T) {
	// The server hands out the same token forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"a"},{"id":"f2","name":"b"}],"next_page_token":"same"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	walk := c.WalkPages(context.Background(), 2, 10)
	if !walk.RepeatedToken {
		t.Fatalf("walk = %+v, want a repeated-token stop", walk)
	}
	if walk.Stopped != "repeated_token" || walk.Pages != 2 {
		t.Fatalf("walk = %+v", walk)
	}
	if walk.DuplicateIDs != 2 {
		t.Fatalf("duplicates = %d, want 2 (second page repeats f1,f2)", walk.DuplicateIDs)
	}
}

func TestWalkPagesStopsWhenExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = io.WriteString(w, `{"files":[{"id":"f1"},{"id":"f2"}],"next_page_token":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"files":[{"id":"f3"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	walk := c.WalkPages(context.Background(), 2, 10)
	if walk.Stopped != "no_more" || walk.Pages != 2 || walk.Items != 3 || walk.DuplicateIDs != 0 {
		t.Fatalf("walk = %+v", walk)
	}
}

func TestDeletePermanentlyPostsIDs(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"task_id":"d1"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	if err := c.DeletePermanently(context.Background(), []string{"f1", "f2"}); err != nil {
		t.Fatalf("DeletePermanently: %v", err)
	}
	if gotPath != "/drive/v1/files:batchDelete" {
		t.Fatalf("path = %s", gotPath)
	}
	ids, _ := gotBody["ids"].([]any)
	if len(ids) != 2 || ids[0] != "f1" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestBurstRunsConcurrentRequests(t *testing.T) {
	var inFlight, peak int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	res := c.Burst(context.Background(), 6)
	if res.Requests != 6 || res.OK != 6 || res.Failed != 0 {
		t.Fatalf("burst = %+v", res)
	}
	if peak < 2 {
		t.Fatalf("requests did not overlap: peak = %d", peak)
	}
}
func TestListTasksSendsPageToken(t *testing.T) {
	var gotQuery map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/drive/v1/tasks" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotQuery = map[string]string{}
		for k := range r.URL.Query() {
			gotQuery[k] = r.URL.Query().Get(k)
		}
		_, _ = io.WriteString(w, `{"tasks":[{"id":"t1","phase":"PHASE_TYPE_RUNNING","progress":42,
			"file_name":"a.mkv","file_size":"99"}],"next_page_token":"n1"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	page, err := c.ListTasks(context.Background(), "t0")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "t1" || page.Items[0].Progress != 42 || page.Items[0].FileSize != 99 {
		t.Fatalf("page = %+v", page)
	}
	if page.NextPageToken != "n1" {
		t.Fatalf("token = %q", page.NextPageToken)
	}
	if gotQuery["page_token"] != "t0" {
		t.Fatalf("query = %v", gotQuery)
	}
	if _, ok := gotQuery["filters"]; ok {
		t.Fatalf("phase filters are rejected upstream and must not be sent: %v", gotQuery)
	}
}

func TestRetryTaskCreatesARetryTask(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	if err := c.RetryTask(context.Background(), "t1"); err != nil {
		t.Fatalf("RetryTask: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/drive/v1/task" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if gotBody["type"] != "offline" || gotBody["create_type"] != "RETRY" || gotBody["id"] != "t1" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestClearTasksPostsPhasesToClearEndpoint(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	if err := c.ClearTasks(context.Background()); err != nil {
		t.Fatalf("ClearTasks: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/drive/v1/tasks:clear" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	// An empty body clears nothing: the official web sends the task type and
	// the phases it wants dropped.
	if gotBody["type"] != "offline" {
		t.Fatalf("body type = %v", gotBody["type"])
	}
	phases, _ := gotBody["phases"].([]any)
	if len(phases) != 2 || phases[0] != "PHASE_TYPE_COMPLETE" || phases[1] != "PHASE_TYPE_ERROR" {
		t.Fatalf("phases = %v", gotBody["phases"])
	}
}

func TestCreateOfflineDailyQuotaIsNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"file_task_quota_exceeded","error_code":11,"error_description":"daily limit"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	_, err := c.CreateOffline(context.Background(), "magnet:?xt=urn:btih:x", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 11 {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (quota errors must not be retried)", got)
	}
}

func TestClearFinishedTasksDeletesEachFinishedTask(t *testing.T) {
	var mu sync.Mutex
	tasks := []map[string]any{
		{"id": "t1", "phase": "PHASE_TYPE_COMPLETE", "type": "offline"},
		{"id": "t2", "phase": "PHASE_TYPE_ERROR", "type": "offline"},
		{"id": "t3", "phase": "PHASE_TYPE_RUNNING", "type": "offline"},
	}
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks, "next_page_token": ""})
		case r.Method == http.MethodDelete && r.URL.Path == "/drive/v1/tasks":
			for _, id := range strings.Split(r.URL.Query().Get("task_ids"), ",") {
				deleted = append(deleted, id)
				kept := tasks[:0]
				for _, task := range tasks {
					if task["id"] != id {
						kept = append(kept, task)
					}
				}
				tasks = kept
			}
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok"})
	n, err := c.ClearFinishedTasks(context.Background())
	if err != nil {
		t.Fatalf("ClearFinishedTasks: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted = %d, want 2", n)
	}
	if len(deleted) != 2 || deleted[0] != "t1" || deleted[1] != "t2" {
		t.Fatalf("deleted ids = %v", deleted)
	}
	if len(tasks) != 1 || tasks[0]["id"] != "t3" {
		t.Fatalf("running task was removed: %v", tasks)
	}
}
