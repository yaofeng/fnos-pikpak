package pikpak

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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