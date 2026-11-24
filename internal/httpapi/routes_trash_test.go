package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecentNormalizesEvents(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"events":[
			{"type":"TYPE_RESTORE","file":{"id":"f1","name":"a.mkv","size":"10"}},
			{"type":"TYPE_RESTORE","file":{"id":"f2","name":"b.txt","size":"5"}}
		],"next_page_token":"e2"}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	// The merged recent listing iterates real account rows.
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/recent", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if gotPath != "/drive/v1/events" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", body["items"])
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["account_id"] != "acct-a" {
			t.Fatalf("item = %v, want the account stamp", item)
		}
	}
	// The envelope token wraps the upstream one, it is not the raw value.
	token, _ := body["next_page_token"].(string)
	if token == "" || token == "e2" {
		t.Fatalf("next_page_token = %v, want the merged envelope", body["next_page_token"])
	}
}

func TestTrashListsWithTrashedFilter(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"files":[{"id":"t1","name":"old.txt"}],"next_page_token":"n2"}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/trash?page_token=abc", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	var filters map[string]map[string]any
	if err := json.Unmarshal([]byte(q["filters"]), &filters); err != nil {
		t.Fatalf("filters %q: %v", q["filters"], err)
	}
	if filters["trashed"]["eq"] != true {
		t.Fatalf("filters = %v", filters)
	}
	if q["page_token"] != "abc" {
		t.Fatalf("page_token = %q", q["page_token"])
	}
	if q["parent_id"] != "" {
		t.Fatalf("trash listing sent parent_id = %q", q["parent_id"])
	}
	if body["next_page_token"] != "n2" {
		t.Fatalf("body = %v", body)
	}
}

func TestEmptyTrashCallsUpstreamAndWaits(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == "/drive/v1/files/trash:empty":
			called = true
			_, _ = io.WriteString(w, `{"task_id":""}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/trash/empty", "1000", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if !called {
		t.Fatal("trash:empty was never called")
	}
	if body["ok"] != true || body["pending"] != false {
		t.Fatalf("body = %v", body)
	}
}

func TestEmptyTrashReportsPendingTask(t *testing.T) {
	old := batchWaitTimeout
	batchWaitTimeout = 30 * time.Millisecond
	t.Cleanup(func() { batchWaitTimeout = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/drive/v1/files/trash:empty"):
			_, _ = io.WriteString(w, `{"task_id":"t9"}`)
		case strings.HasPrefix(r.URL.Path, "/drive/v1/tasks/"):
			_, _ = io.WriteString(w, `{"id":"t9","phase":"PHASE_TYPE_RUNNING"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/trash/empty", "1000", `{}`)
	if rec.Code != http.StatusOK || body["pending"] != true {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestTrashAndOpsFollowAccount(t *testing.T) {
	// GET /api/trash、POST /api/trash/empty、POST /api/files/trash、POST /api/folders
	// 带 account= 时打到对应上游：fake server 按 bearer token 记录每个上游身份
	// 收到的请求。
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Header.Get("Authorization")+" "+r.Method+" "+r.URL.Path]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/drive/v1/files/trash:empty"):
			_, _ = io.WriteString(w, `{"task_id":""}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/drive/v1/files:"):
			_, _ = io.WriteString(w, `{"task_id":""}`)
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{"file":{"id":"new1","name":"n","kind":"drive#folder"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{"files":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	if rec, body := getJSON(t, h, "/api/trash?account=acct-b", "1000"); rec.Code != http.StatusOK {
		t.Fatalf("trash status = %d body=%v", rec.Code, body)
	}
	if rec, body := postJSON(t, h, "/api/trash/empty?account=acct-b", "1000", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("trash/empty status = %d body=%v", rec.Code, body)
	}
	if rec, body := postJSON(t, h, "/api/files/trash?account=acct-b", "1000", `{"ids":["f1"]}`); rec.Code != http.StatusOK {
		t.Fatalf("files/trash status = %d body=%v", rec.Code, body)
	}
	if rec, body := postJSON(t, h, "/api/folders?account=acct-b", "1000", `{"parent_id":"p1","name":"n"}`); rec.Code != http.StatusCreated {
		t.Fatalf("folders status = %d body=%v", rec.Code, body)
	}

	want := map[string]bool{
		"Bearer at-acct-b GET /drive/v1/files":               true,
		"Bearer at-acct-b PATCH /drive/v1/files/trash:empty": true,
		"Bearer at-acct-b POST /drive/v1/files:batchTrash":   true,
		"Bearer at-acct-b POST /drive/v1/files":              true,
	}
	mu.Lock()
	defer mu.Unlock()
	for key := range seen {
		if !want[key] {
			t.Fatalf("unexpected upstream call %s (seen=%v)", key, seen)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("upstream calls missing: %v (seen=%v)", want, seen)
	}
}

func TestRecentRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/recent", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}
