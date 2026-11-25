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

// TestTrashRouteMergedAll pins the all-accounts merged trash listing: every
// active account's trash page merges into one response, each row stamped with
// the account that served it, and the upstream still sees the trashed filter.
func TestTrashRouteMergedAll(t *testing.T) {
	// 一个监听地址按 bearer 扮演两个上游（照 files 合并测试的模式），各自的
	// 回收站里有一行；同时记录每个上游收到的 filters。
	var mu sync.Mutex
	filters := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		filters[r.Header.Get("Authorization")] = r.URL.Query().Get("filters")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer at-acct-a":
			_, _ = io.WriteString(w, `{"files":[{"id":"ta1","name":"a-old.mkv","kind":"drive#file","size":"1"}]}`)
		case "Bearer at-acct-b":
			_, _ = io.WriteString(w, `{"files":[{"id":"tb1","name":"b-old.mkv","kind":"drive#file","size":"2"}]}`)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/trash?account=all", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v, want one row per account", body["items"])
	}
	stamps := map[string]string{} // file id → account_id
	names := map[string]string{}  // account_id → account_name
	for _, raw := range items {
		file, _ := raw.(map[string]any)
		id, _ := file["account_id"].(string)
		stamps[file["id"].(string)] = id
		names[id] = file["account_name"].(string)
	}
	if stamps["ta1"] != "acct-a" || stamps["tb1"] != "acct-b" {
		t.Fatalf("stamps = %v, want ta1→acct-a / tb1→acct-b", stamps)
	}
	if names["acct-a"] != "账号acct-a" || names["acct-b"] != "账号acct-b" {
		t.Fatalf("names = %v, want 账号<id> on every row", names)
	}
	if _, ok := body["account_errors"]; ok {
		t.Fatalf("account_errors = %v, want the key absent on a clean merge", body["account_errors"])
	}
	// 两个上游都必须带着 trashed 过滤器被问到（trashFilters 复用）。
	mu.Lock()
	defer mu.Unlock()
	for bearer, raw := range filters {
		var parsed struct {
			Trashed struct{ Eq bool } `json:"trashed"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			t.Fatalf("%s filters %q: %v", bearer, raw, err)
		}
		if !parsed.Trashed.Eq {
			t.Fatalf("%s filters = %q, want trashed eq true", bearer, raw)
		}
	}
	if len(filters) != 2 {
		t.Fatalf("upstreams asked = %v, want both accounts", filters)
	}
}

// TestTrashMergedPaginationNoRepeat pins the merged trash page-token envelope:
// an account whose listing ended is recorded in the token (pageDone) and never
// asked again, so its first page cannot re-appear on later pages.
func TestTrashMergedPaginationNoRepeat(t *testing.T) {
	var mu sync.Mutex
	// 每个 bearer 身份被请求过的上游 page_token，按顺序记录。
	asked := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		page := r.URL.Query().Get("page_token")
		// 并发 handler 共享这张表：计数必须在锁内取，解锁后再读就是数据竞争。
		mu.Lock()
		asked[token] = append(asked[token], page)
		n := len(asked[token])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch token {
		case "at-acct-a":
			// 第一次回 next_page_token="p2"，第二次返回空 token。第 2、3 页的
			// 信封都携带上游 token "p2"，所以按调用次数而非 token 值区分。
			switch n {
			case 1:
				_, _ = io.WriteString(w, `{"files":[{"id":"ta1","name":"ta1.mkv","kind":"drive#file","modified_time":"2026-01-02T00:00:00Z"}],"next_page_token":"p2"}`)
			case 2:
				_, _ = io.WriteString(w, `{"files":[{"id":"ta2","name":"ta2.mkv","kind":"drive#file","modified_time":"2026-01-01T00:00:00Z"}]}`)
			default:
				_, _ = io.WriteString(w, `{"files":[]}`)
			}
		default:
			// acct-b 一页即尽：首页后就没了。
			_, _ = io.WriteString(w, `{"files":[{"id":"tb1","name":"tb1.mkv","kind":"drive#file","modified_time":"2026-01-03T00:00:00Z"}]}`)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	// 第一页：两个账号都有行，信封 token 非空。
	rec, body := getJSON(t, h, "/api/trash?account=all", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	pageIDs := func(body map[string]any) []string {
		t.Helper()
		items, _ := body["items"].([]any)
		var ids []string
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			ids = append(ids, id)
		}
		return ids
	}
	if ids := pageIDs(body); len(ids) != 2 {
		t.Fatalf("page 1 items = %v, want ta1 and tb1", ids)
	}
	token, _ := body["next_page_token"].(string)
	if token == "" || token == "p2" {
		t.Fatalf("next_page_token = %v, want the merged envelope", body["next_page_token"])
	}

	// 第二页：只含仍有页的账号；acct-b 已尽，tb1 不得重复，上游也不再被问。
	rec, body = getJSON(t, h, "/api/trash?account=all&page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 1 || ids[0] != "ta2" {
		t.Fatalf("page 2 items = %v, want only ta2 (tb1 must not repeat)", ids)
	}
	mu.Lock()
	if got := asked["at-acct-b"]; len(got) != 1 {
		mu.Unlock()
		t.Fatalf("exhausted account asked %v times, want exactly once", got)
	}
	mu.Unlock()

	// 第三页带同样的 token：acct-b 由 pageDone 跳过，acct-a 再问已是空页 →
	// items 为空、next_page_token 缺省。
	rec, body = getJSON(t, h, "/api/trash?account=all&page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 3 status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 0 {
		t.Fatalf("page 3 items = %v, want empty", ids)
	}
	if _, ok := body["next_page_token"]; ok {
		t.Fatalf("page 3 carried next_page_token = %v, want it omitted", body["next_page_token"])
	}
	if errs, ok := body["account_errors"]; ok {
		t.Fatalf("page 3 reported account_errors = %v", errs)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := asked["at-acct-b"]; len(got) != 1 {
		t.Fatalf("exhausted account asked %v times total, want exactly once", got)
	}
}

// TestTrashMergedAccountErrorIsolated pins error isolation in the merged
// trash listing: one dead upstream cannot blank the healthy account's rows,
// and the failure surfaces as exactly one account_errors entry naming the
// dead one.
func TestTrashMergedAccountErrorIsolated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer at-acct-b" {
			// Retry-After: 0 keeps the client's 500-retry loop from sleeping.
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"files":[{"id":"ta1","name":"a-old.mkv","kind":"drive#file","modified_time":"2026-01-02T00:00:00Z"}]}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/trash?account=all", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want acct-a's row despite acct-b's failure", body["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["account_id"] != "acct-a" {
		t.Fatalf("stamp = %v, want acct-a", first["account_id"])
	}
	errs, _ := body["account_errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("account_errors = %v, want exactly one entry", body["account_errors"])
	}
	e, _ := errs[0].(map[string]any)
	if e["account_id"] != "acct-b" {
		t.Fatalf("account_errors[0].account_id = %v, want acct-b", e["account_id"])
	}
	if msg, _ := e["error"].(string); msg == "" {
		t.Fatalf("account_errors[0].error = %v, want a message", e["error"])
	}
}
