package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// opsServer answers the PikPak endpoints the file-operation routes use.
func opsServer(t *testing.T, taskPhase string, captured *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if captured != nil {
			*captured = append(*captured, r.Method+" "+r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{"file":{"id":"new1","name":"n","kind":"drive#folder"}}`)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/drive/v1/files/"):
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/drive/v1/files:"):
			_, _ = io.WriteString(w, `{"task_id":"t1"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/drive/v1/tasks/"):
			_, _ = io.WriteString(w, `{"id":"t1","phase":"`+taskPhase+`"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func postJSON(t *testing.T, h http.Handler, path, uid, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Trim-Userid", uid)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestCreateFolderRoute(t *testing.T) {
	srv := opsServer(t, "PHASE_TYPE_COMPLETE", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/folders", "1000", `{"parent_id":"p1","name":"新建文件夹"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	file, _ := body["file"].(map[string]any)
	if file["id"] != "new1" {
		t.Fatalf("body = %v", body)
	}
}

func TestCreateFolderRejectsBlankName(t *testing.T) {
	srv := opsServer(t, "PHASE_TYPE_COMPLETE", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/folders", "1000", `{"name":"   "}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "bad_name" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestRenameRoute(t *testing.T) {
	var calls []string
	srv := opsServer(t, "PHASE_TYPE_COMPLETE", &calls)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPatch, "/api/files/f1", strings.NewReader(`{"name":"新名字"}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body)
	}
	if len(calls) == 0 || calls[0] != "PATCH /drive/v1/files/f1" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestRenameRejectsBlankName(t *testing.T) {
	srv := opsServer(t, "PHASE_TYPE_COMPLETE", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPatch, "/api/files/f1", strings.NewReader(`{"name":""}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestBatchMoveWaitsForTask(t *testing.T) {
	old := batchWaitTimeout
	batchWaitTimeout = 2 * time.Second
	t.Cleanup(func() { batchWaitTimeout = old })

	srv := opsServer(t, "PHASE_TYPE_COMPLETE", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/files/move", "1000", `{"ids":["a","b"],"to_parent_id":"p2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["pending"] != false || body["task_id"] != "t1" || body["ok"] != true {
		t.Fatalf("body = %v", body)
	}
}

func TestBatchMoveReportsPendingAfterTimeout(t *testing.T) {
	old := batchWaitTimeout
	batchWaitTimeout = 30 * time.Millisecond
	t.Cleanup(func() { batchWaitTimeout = old })

	srv := opsServer(t, "PHASE_TYPE_RUNNING", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/files/move", "1000", `{"ids":["a"],"to_parent_id":"p2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["pending"] != true {
		t.Fatalf("a still-running task was reported as finished: %v", body)
	}
}

func TestBatchIDsValidation(t *testing.T) {
	srv := opsServer(t, "PHASE_TYPE_COMPLETE", nil)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := postJSON(t, h, "/api/files/trash", "1000", `{"ids":[]}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "bad_ids" {
		t.Fatalf("empty ids: %d %v", rec.Code, body)
	}

	many := make([]string, maxBatchIDs+1)
	for i := range many {
		many[i] = "id" + strconv.Itoa(i)
	}
	raw, _ := json.Marshal(map[string]any{"ids": many})
	rec, body = postJSON(t, h, "/api/files/trash", "1000", string(raw))
	if rec.Code != http.StatusBadRequest || errCode(body) != "too_many_ids" {
		t.Fatalf("too many ids: %d %v", rec.Code, body)
	}

	rec, body = postJSON(t, h, "/api/files/move", "1000", `{"ids":["p2"],"to_parent_id":"p2"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "bad_target" {
		t.Fatalf("move into itself: %d %v", rec.Code, body)
	}
}

func TestBatchIDsAreDeduplicated(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/drive/v1/files:") {
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(b))
			_, _ = io.WriteString(w, `{"task_id":"t1"}`)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/drive/v1/tasks/") {
			_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_COMPLETE"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	if rec, body := postJSON(t, h, "/api/files/trash", "1000", `{"ids":["a","a","b"]}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if len(bodies) != 1 {
		t.Fatalf("upstream calls = %v", bodies)
	}
	var sent struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal([]byte(bodies[0]), &sent)
	if len(sent.IDs) != 2 || sent.IDs[0] != "a" || sent.IDs[1] != "b" {
		t.Fatalf("ids = %v", sent.IDs)
	}
}
