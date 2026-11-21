package pikpak

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// opRecorder captures one request and answers with a canned body.
type opRecorder struct {
	mu     sync.Mutex
	method string
	path   string
	body   map[string]any
	raw    string
}

func (r *opRecorder) server(t *testing.T, status int, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.method = req.Method
		r.path = req.URL.Path
		r.raw = ""
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			r.raw = string(b)
			_ = json.Unmarshal(b, &r.body)
		}
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
}

func (r *opRecorder) check(t *testing.T, method, path string) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.method != method || r.path != path {
		t.Fatalf("request = %s %s, want %s %s", r.method, r.path, method, path)
	}
	return r.body
}

func idsOf(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["ids"].([]any)
	if !ok {
		t.Fatalf("body has no ids: %v", body)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

func TestCreateFolder(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{"upload_type":"UPLOAD_TYPE_UNKNOWN","file":{"id":"f1","name":"新文件夹","kind":"drive#folder"}}`)
	defer srv.Close()

	f, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).CreateFolder(context.Background(), "p1", "新文件夹")
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if f.ID != "f1" || f.Name != "新文件夹" {
		t.Fatalf("file = %+v", f)
	}
	body := rec.check(t, http.MethodPost, "/drive/v1/files")
	if body["kind"] != "drive#folder" || body["name"] != "新文件夹" || body["parent_id"] != "p1" {
		t.Fatalf("body = %v", body)
	}
}

func TestRename(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{}`)
	defer srv.Close()

	if err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).Rename(context.Background(), "f1", "n2"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	body := rec.check(t, http.MethodPatch, "/drive/v1/files/f1")
	if body["name"] != "n2" {
		t.Fatalf("body = %v", body)
	}
}

func TestBatchOperations(t *testing.T) {
	cases := []struct {
		name   string
		call   func(c *Client) (string, error)
		path   string
		method string
		body   func(t *testing.T, b map[string]any)
	}{
		{
			name: "move", path: "/drive/v1/files:batchMove", method: http.MethodPost,
			call: func(c *Client) (string, error) { return c.BatchMove(context.Background(), []string{"a", "b"}, "p2") },
			body: func(t *testing.T, b map[string]any) {
				if got := idsOf(t, b); len(got) != 2 || got[0] != "a" {
					t.Fatalf("ids = %v", got)
				}
				to, _ := b["to"].(map[string]any)
				if to["parent_id"] != "p2" {
					t.Fatalf("to = %v", to)
				}
			},
		},
		{
			name: "copy", path: "/drive/v1/files:batchCopy", method: http.MethodPost,
			call: func(c *Client) (string, error) { return c.BatchCopy(context.Background(), []string{"a"}, "p2") },
			body: func(t *testing.T, b map[string]any) {
				if len(idsOf(t, b)) != 1 {
					t.Fatalf("ids = %v", b["ids"])
				}
			},
		},
		{
			name: "trash", path: "/drive/v1/files:batchTrash", method: http.MethodPost,
			call: func(c *Client) (string, error) { return c.BatchTrash(context.Background(), []string{"a"}) },
			body: func(t *testing.T, b map[string]any) {
				if len(idsOf(t, b)) != 1 {
					t.Fatalf("ids = %v", b["ids"])
				}
			},
		},
		{
			name: "untrash", path: "/drive/v1/files:batchUntrash", method: http.MethodPost,
			call: func(c *Client) (string, error) { return c.BatchUntrash(context.Background(), []string{"a"}) },
			body: func(t *testing.T, b map[string]any) {},
		},
		{
			name: "delete", path: "/drive/v1/files:batchDelete", method: http.MethodPost,
			call: func(c *Client) (string, error) { return c.BatchDelete(context.Background(), []string{"a"}) },
			body: func(t *testing.T, b map[string]any) {},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &opRecorder{}
			srv := rec.server(t, http.StatusOK, `{"task_id":"t1"}`)
			defer srv.Close()

			id, err := tc.call(newTestClient(t, srv.URL, &fakeTokens{token: "t"}))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if id != "t1" {
				t.Fatalf("task id = %q", id)
			}
			body := rec.check(t, tc.method, tc.path)
			tc.body(t, body)
		})
	}
}

func TestBatchMoveToRootOmitsTarget(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{"task_id":"t1"}`)
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).BatchMove(context.Background(), []string{"a"}, "")
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	body := rec.check(t, http.MethodPost, "/drive/v1/files:batchMove")
	if to, ok := body["to"]; ok {
		t.Fatalf("root move sent a target: %v", to)
	}
}

func TestStarAndUnstar(t *testing.T) {
	for _, tc := range []struct {
		path string
		call func(c *Client) error
	}{
		{"/drive/v1/files:star", func(c *Client) error { return c.Star(context.Background(), []string{"a"}) }},
		{"/drive/v1/files:unstar", func(c *Client) error { return c.Unstar(context.Background(), []string{"a"}) }},
	} {
		rec := &opRecorder{}
		srv := rec.server(t, http.StatusOK, `{}`)
		if err := tc.call(newTestClient(t, srv.URL, &fakeTokens{token: "t"})); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		rec.check(t, http.MethodPost, tc.path)
		srv.Close()
	}
}

func TestEmptyTrash(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{"task_id":""}`)
	defer srv.Close()

	id, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).EmptyTrash(context.Background())
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if id != "" {
		t.Fatalf("task id = %q", id)
	}
	rec.check(t, http.MethodPatch, "/drive/v1/files/trash:empty")
}

func TestFileDetail(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{"id":"f1","name":"movie.mkv","size":"42","phase":"PHASE_TYPE_COMPLETE"}`)
	defer srv.Close()

	f, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).FileDetail(context.Background(), "f1")
	if err != nil {
		t.Fatalf("FileDetail: %v", err)
	}
	if f.ID != "f1" || f.Name != "movie.mkv" || f.Size != 42 {
		t.Fatalf("file = %+v", f)
	}
	rec.check(t, http.MethodGet, "/drive/v1/files/f1")
}

func TestBatchOperationRejectsErrorEnvelope(t *testing.T) {
	rec := &opRecorder{}
	srv := rec.server(t, http.StatusOK, `{"error":"file_invalid_file_filters","error_code":3}`)
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).BatchTrash(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("a 200 carrying an error envelope was treated as success")
	}
}

func TestWaitTaskSettledReportsCompletion(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		phase := "PHASE_TYPE_RUNNING"
		if n >= 2 {
			phase = "PHASE_TYPE_COMPLETE"
		}
		_, _ = io.WriteString(w, `{"id":"t1","phase":"`+phase+`"}`)
	}))
	defer srv.Close()

	task, done, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).
		WaitTaskSettled(context.Background(), "t1", 2*time.Second, 10*time.Millisecond)
	if err != nil || !done {
		t.Fatalf("done=%v err=%v task=%+v", done, err, task)
	}
	if task.Phase != "PHASE_TYPE_COMPLETE" {
		t.Fatalf("phase = %q", task.Phase)
	}
}

func TestWaitTaskSettledGivesUpWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_RUNNING"}`)
	}))
	defer srv.Close()

	task, done, err := newTestClient(t, srv.URL, &fakeTokens{token: "t"}).
		WaitTaskSettled(context.Background(), "t1", 50*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("timeout returned an error: %v", err)
	}
	if done {
		t.Fatal("timeout reported completion")
	}
	if task.Phase != "PHASE_TYPE_RUNNING" {
		t.Fatalf("phase = %q", task.Phase)
	}
}
