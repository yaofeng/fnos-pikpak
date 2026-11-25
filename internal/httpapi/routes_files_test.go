package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"fnos-pikpak/internal/pikpak"
)

// filesTestServer records the query the list endpoint sent and answers with
// the body provided.
func filesTestServer(t *testing.T, capture *map[string]string, status int, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		*capture = q
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
}

func TestFilesRouteFollowsAccountParam(t *testing.T) {
	// 两个账号、两个 fake 上游返回不同文件名集合：一个监听地址按 bearer token
	// 分成两个上游身份，各自回答自己的文件集合。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer at-acct-a":
			_, _ = io.WriteString(w, `{"files":[{"id":"fa1","name":"a.mkv","kind":"drive#file","size":"1"}]}`)
		case "Bearer at-acct-b":
			_, _ = io.WriteString(w, `{"files":[{"id":"fb1","name":"b.mkv","kind":"drive#file","size":"2"}]}`)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	firstName := func(rec *httptest.ResponseRecorder, body map[string]any) string {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%v", rec.Code, body)
		}
		items, _ := body["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("items = %v", body["items"])
		}
		file, _ := items[0].(map[string]any)
		return file["name"].(string)
	}

	// GET /api/files?account=a → 集合 A；account=b → 集合 B；缺省 → 集合 A
	rec, body := getJSON(t, h, "/api/files?account=acct-a", "1000")
	if got := firstName(rec, body); got != "a.mkv" {
		t.Fatalf("account=acct-a served %q, want a.mkv", got)
	}
	rec, body = getJSON(t, h, "/api/files?account=acct-b", "1000")
	if got := firstName(rec, body); got != "b.mkv" {
		t.Fatalf("account=acct-b served %q, want b.mkv", got)
	}
	rec, body = getJSON(t, h, "/api/files", "1000")
	if got := firstName(rec, body); got != "a.mkv" {
		t.Fatalf("param-less served %q, want the default account's a.mkv", got)
	}
}

func TestAccountParamRejectsForeignID(t *testing.T) {
	// 一个监听地址同时扮演两个上游：uid 1001 请求另一个 uid 的账号 id 时，
	// 两个上游都必须零调用。
	var upstreamCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 2000, "acct-foreign", "sub-foreign", "active")
	bindAccount(t, st, 1001, "acct-mine", "sub-mine", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?account=acct-foreign", "1001")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%v, want 404 not_found", rec.Code, body)
	}
	if errCode(body) != "not_found" {
		t.Fatalf("code = %v, want not_found", errCode(body))
	}
	if got := atomic.LoadInt32(&upstreamCalls); got != 0 {
		t.Fatalf("a foreign account id reached the upstream %d times, want 0", got)
	}
}

func TestFilesRequireBinding(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/files", "1000")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestFilesSendsKindFilterAndLimit(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"files":[{"id":"f1","name":"a.mkv","kind":"drive#file","size":"42"}],"next_page_token":"n1"}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?parent_id=p1&kind=drive%23folder&limit=500", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if q["parent_id"] != "p1" {
		t.Fatalf("parent_id = %q", q["parent_id"])
	}
	if q["limit"] != "200" {
		t.Fatalf("limit = %q, want capped 200", q["limit"])
	}
	var filters struct {
		Kind    struct{ Eq string } `json:"kind"`
		Trashed struct{ Eq bool }   `json:"trashed"`
	}
	if err := json.Unmarshal([]byte(q["filters"]), &filters); err != nil {
		t.Fatalf("filters %q: %v", q["filters"], err)
	}
	if filters.Kind.Eq != "drive#folder" {
		t.Fatalf("filters = %v", filters)
	}
	if filters.Trashed.Eq {
		t.Fatalf("trashed filter must be boolean false: %s", q["filters"])
	}
	if _, ok := q["order"]; ok {
		t.Fatalf("order was sent even though PikPak rejects every encoding: %v", q)
	}

	if body["next_page_token"] != "n1" {
		t.Fatalf("body = %v", body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
}

func TestFilesExcludesTrashedByDefault(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"files":[]}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?parent_id=", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	var filters struct {
		Trashed struct{ Eq bool } `json:"trashed"`
	}
	if err := json.Unmarshal([]byte(q["filters"]), &filters); err != nil {
		t.Fatalf("filters %q: %v", q["filters"], err)
	}
	if filters.Trashed.Eq {
		t.Fatalf("trashed filter must be boolean false: %s", q["filters"])
	}
}

func TestFilesQueryFiltersOnlyTheLoadedPage(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{
		"files":[
			{"id":"f1","name":"Holiday.MKV"},
			{"id":"f2","name":"notes.txt"},
			{"id":"f3","name":"holiday-list.txt"}
		],
		"next_page_token":"n2"
	}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?q=holiday", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v, want the two names containing holiday", items)
	}
	if body["q_local"] != true {
		t.Fatalf("q_local = %v", body["q_local"])
	}
	if body["next_page_token"] != "n2" {
		t.Fatalf("filtering dropped the page token: %v", body["next_page_token"])
	}
	// The query must not be forwarded as a server-side search.
	if _, ok := q["q"]; ok {
		t.Fatalf("q was forwarded upstream: %v", q)
	}
}

func TestFilesUpstreamErrorEnvelopeIs502(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"error":"invalid_argument","error_code":3}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files", "1000")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%v, want 502", rec.Code, body)
	}
}

func TestFilesLimitDefaultsTo100(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"files":[]}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)
	getJSON(t, h, "/api/files", "1000")
	if q["limit"] != "100" {
		t.Fatalf("limit = %q, want 100", q["limit"])
	}
}

func TestFilesRejectsBadLimit(t *testing.T) {
	var q map[string]string
	srv := filesTestServer(t, &q, http.StatusOK, `{"files":[]}`)
	defer srv.Close()

	deps, _ := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, deps.Store, 1000)
	h := NewMux(deps)
	rec, _ := getJSON(t, h, "/api/files?limit=abc", "1000")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "bad_limit") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestUpstreamRateLimitIs429(t *testing.T) {
	// PikPak rate-limits requests without setting its offline-quota code; the
	// refusal is expected and the client must be told to wait, not that the
	// upstream is broken.
	rec := httptest.NewRecorder()
	writeUpstreamError(rec, &pikpak.APIError{Status: http.StatusTooManyRequests})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "too_many_requests" {
		t.Fatalf("code = %q, want too_many_requests", body.Error.Code)
	}
}

func TestUpstreamReauthIsUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUpstreamError(rec, pikpak.ErrReauthRequired)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "pikpak_reauth_required" {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if !strings.Contains(body.Error.Message, "重新绑定") {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

// TestFilesRouteMergedAll pins the all-accounts merged listing: every active
// account's root page merges into one response, each row stamped with the
// account that served it, and a healthy pair of accounts reports no
// account_errors key at all.
func TestFilesRouteMergedAll(t *testing.T) {
	// 两个账号、一个监听地址按 bearer 扮演两个上游，各自回答自己的文件集合
	// （照 TestFilesRouteFollowsAccountParam 的模式）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer at-acct-a":
			_, _ = io.WriteString(w, `{"files":[{"id":"fa1","name":"a.mkv","kind":"drive#file","size":"1"}]}`)
		case "Bearer at-acct-b":
			_, _ = io.WriteString(w, `{"files":[{"id":"fb1","name":"b.mkv","kind":"drive#file","size":"2"}]}`)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?account=all", "1000")
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
	if stamps["fa1"] != "acct-a" || stamps["fb1"] != "acct-b" {
		t.Fatalf("stamps = %v, want fa1→acct-a / fb1→acct-b", stamps)
	}
	if names["acct-a"] != "账号acct-a" || names["acct-b"] != "账号acct-b" {
		t.Fatalf("names = %v, want 账号<id> on every row", names)
	}
	if _, ok := body["account_errors"]; ok {
		t.Fatalf("account_errors = %v, want the key absent on a clean merge", body["account_errors"])
	}
}

// TestFilesMergedPaginationNoRepeat pins the merged page-token envelope: an
// account whose listing ended is recorded in the token (pageDone) and never
// asked again, so its first page cannot re-appear on later pages.
func TestFilesMergedPaginationNoRepeat(t *testing.T) {
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
		mu.Lock()
		asked[token] = append(asked[token], page)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch token {
		case "at-acct-a":
			// 第一次列目录回 next_page_token="p2"，第二次返回空 token。第 2、3
			// 页的信封都携带上游 token "p2"，所以按调用次数而非 token 值区分。
			switch len(asked[token]) {
			case 1:
				_, _ = io.WriteString(w, `{"files":[{"id":"a1","name":"a1.mkv","kind":"drive#file","modified_time":"2026-01-02T00:00:00Z"}],"next_page_token":"p2"}`)
			case 2:
				_, _ = io.WriteString(w, `{"files":[{"id":"a2","name":"a2.mkv","kind":"drive#file","modified_time":"2026-01-01T00:00:00Z"}]}`)
			default:
				// 之后的调用：目录已彻底走完，空页。
				_, _ = io.WriteString(w, `{"files":[]}`)
			}
		default:
			// acct-b 一页即尽：首页后就没了。
			_, _ = io.WriteString(w, `{"files":[{"id":"b1","name":"b1.mkv","kind":"drive#file","modified_time":"2026-01-03T00:00:00Z"}]}`)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	// 第一页：两个账号都有行，信封 token 非空。
	rec, body := getJSON(t, h, "/api/files?account=all", "1000")
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
		t.Fatalf("page 1 items = %v, want a1 and b1", ids)
	}
	token, _ := body["next_page_token"].(string)
	if token == "" || token == "p2" {
		t.Fatalf("next_page_token = %v, want the merged envelope", body["next_page_token"])
	}

	// 第二页：只含第二页行；acct-b 已尽，b1 不得重复，上游也不再被问。
	rec, body = getJSON(t, h, "/api/files?account=all&page_token="+token, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d body=%v", rec.Code, body)
	}
	if ids := pageIDs(body); len(ids) != 1 || ids[0] != "a2" {
		t.Fatalf("page 2 items = %v, want only a2 (b1 must not repeat)", ids)
	}
	mu.Lock()
	if got := asked["at-acct-b"]; len(got) != 1 {
		mu.Unlock()
		t.Fatalf("exhausted account asked %v times, want exactly once", got)
	}
	mu.Unlock()

	// 第三页带同样的 token：acct-b 由 pageDone 跳过，acct-a 再问已是空页 →
	// items 为空、next_page_token 缺省。
	rec, body = getJSON(t, h, "/api/files?account=all&page_token="+token, "1000")
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
	if got := asked["at-acct-b"]; len(got) != 1 {
		t.Fatalf("exhausted account asked %v times total, want exactly once", got)
	}
}

// TestFilesMergedAccountErrorIsolated pins error isolation in the merged
// listing: one dead upstream cannot blank the healthy account's rows, and the
// failure surfaces as exactly one account_errors entry naming the dead one.
func TestFilesMergedAccountErrorIsolated(t *testing.T) {
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
		_, _ = io.WriteString(w, `{"files":[{"id":"fa1","name":"a.mkv","kind":"drive#file","modified_time":"2026-01-02T00:00:00Z"}]}`)
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	bindAccount(t, st, 1000, "acct-a", "sub-a", "active")
	bindAccount(t, st, 1000, "acct-b", "sub-b", "active")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/files?account=all", "1000")
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
