package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
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
	if body["next_page_token"] != "e2" {
		t.Fatalf("token = %v", body["next_page_token"])
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

func TestRecentRequiresBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/recent", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}
