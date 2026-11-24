package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
