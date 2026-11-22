package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
