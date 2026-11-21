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
	"testing"
	"time"
)

type fakeTokens struct {
	mu     sync.Mutex
	token  string
	forces int
}

func (f *fakeTokens) Token(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token, nil
}

func (f *fakeTokens) ForceRefresh(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forces++
	f.token = "fresh-token"
	return nil
}

func newTestClient(t *testing.T, url string, tokens TokenSource) *Client {
	t.Helper()
	return NewClient(ClientOptions{BaseDrive: url, BaseUser: url, Session: tokens, DeviceID: "dev"})
}

func TestListFilesQueryAndParse(t *testing.T) {
	var gotQuery map[string]string
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" {
			t.Errorf("path = %q", r.URL.Path)
		}
		gotQuery = map[string]string{}
		for k := range r.URL.Query() {
			gotQuery[k] = r.URL.Query().Get(k)
		}
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"kind": "drive#fileList",
			"files": [
				{"id": "f1", "parent_id": "p1", "name": "a.mp4", "kind": "drive#file", "size": "1234567",
				 "phase": "PHASE_TYPE_COMPLETE", "file_category": "VIDEO", "mime_type": "video/mp4",
				 "modified_time": "2026-01-01T00:00:00.000+08:00"},
				{"id": "f2", "parent_id": "p1", "name": "b", "kind": "drive#folder", "size": ""}
			],
			"next_page_token": "next-1"
		}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "tok-1"})
	filters := `{"phase":{"eq":"PHASE_TYPE_COMPLETE"}}`
	page, err := c.ListFiles(context.Background(), "p1", ListOptions{
		PageToken: "pt-1", Limit: 50, Order: "-size", Filters: filters,
	})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	if gotQuery["parent_id"] != "p1" || gotQuery["limit"] != "50" ||
		gotQuery["page_token"] != "pt-1" || gotQuery["order"] != "-size" {
		t.Fatalf("query = %v", gotQuery)
	}
	if gotQuery["filters"] != filters {
		t.Fatalf("filters = %q, want raw %q", gotQuery["filters"], filters)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotUA == "" || !strings.Contains(gotUA, "pikpak-fnos") {
		t.Fatalf("User-Agent = %q", gotUA)
	}

	if len(page.Items) != 2 || page.Items[0].Size != 1234567 || page.Items[0].Category != "VIDEO" {
		t.Fatalf("items = %+v", page.Items)
	}
	if page.Items[1].Size != 0 || page.Items[1].Kind != "drive#folder" {
		t.Fatalf("folder item = %+v", page.Items[1])
	}
	if page.NextPageToken != "next-1" {
		t.Fatalf("next = %q", page.NextPageToken)
	}
}

func TestListFilesRefreshesOn401Once(t *testing.T) {
	var calls int32
	var sawAuth []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		sawAuth = append(sawAuth, r.Header.Get("Authorization"))
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"unauthenticated","error_code":16,"error_description":"token invalid"}`)
			return
		}
		_, _ = io.WriteString(w, `{"files":[],"next_page_token":""}`)
	}))
	defer srv.Close()

	tokens := &fakeTokens{token: "stale"}
	c := newTestClient(t, srv.URL, tokens)
	if _, err := c.ListFiles(context.Background(), "", ListOptions{Limit: 10}); err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if tokens.forces != 1 {
		t.Fatalf("force refreshes = %d, want 1", tokens.forces)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sawAuth) != 2 || sawAuth[0] != "Bearer stale" || sawAuth[1] != "Bearer fresh-token" {
		t.Fatalf("auth headers = %v", sawAuth)
	}
}

func TestListFilesInvalidGrantDoesNotRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_code":4126,"error_description":"reauth"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	_, err := c.ListFiles(context.Background(), "", ListOptions{})
	if !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("err = %v, want ErrReauthRequired", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestListFilesBacksOffOn429(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls == 0 {
			calls++
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"too_many_requests","error_code":10}`)
			return
		}
		calls++
		_, _ = io.WriteString(w, `{"files":[],"next_page_token":""}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	if _, err := c.ListFiles(context.Background(), "", ListOptions{}); err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAboutAndVIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/about":
			_, _ = io.WriteString(w, `{"kind":"drive#about","quota":{"limit":"10995116277760","usage":"5113157556024"},"expires_at":"2027-01-01T00:00:00Z"}`)
		case "/drive/v1/privilege/vip":
			_, _ = io.WriteString(w, `{"result":"ok","message":"","data":{"status":"ok","type":"platinum","expire":"2027-02-01T00:00:00Z"}}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	about, err := c.About(context.Background())
	if err != nil {
		t.Fatalf("About: %v", err)
	}
	if about.QuotaLimit != 10995116277760 || about.QuotaUsage != 5113157556024 || about.ExpiresAt == "" {
		t.Fatalf("about = %+v", about)
	}

	vip, err := c.VIP(context.Background())
	if err != nil {
		t.Fatalf("VIP: %v", err)
	}
	if vip.Type != "platinum" || vip.Expire == "" {
		t.Fatalf("vip = %+v", vip)
	}
}

func TestEventsParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/events" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("next_page_token") != "e1" {
			t.Errorf("next_page_token = %q", r.URL.Query().Get("next_page_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"events":[{"type":"TYPE_RESTORE","file":{"id":"f9","name":"x.mkv","size":"10"}}],"next_page_token":"e2"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	page, err := c.Events(context.Background(), "e1")
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Name != "x.mkv" || page.NextPageToken != "e2" {
		t.Fatalf("events = %+v", page)
	}
}

func TestOrderCandidates(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order := r.URL.Query().Get("order")
		mu.Lock()
		seen[order]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if order == `"-size"` {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_request","error_code":1}`)
			return
		}
		name, _ := json.Marshal("first-" + order)
		_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":`+string(name)+`,"size":"1"}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	results := c.OrderCandidates(context.Background())
	if len(results) != len(orderCandidates) {
		t.Fatalf("results = %d, want %d", len(results), len(orderCandidates))
	}
	if results[0].Order != "name" || results[0].Status != 200 || results[0].FirstName != "first-name" {
		t.Fatalf("name candidate = %+v", results[0])
	}
	if results[1].Order != `"name"` || results[1].Status != 200 || results[1].FirstName != `first-"name"` {
		t.Fatalf(`"name" candidate = %+v`, results[1])
	}
	if results[2].Order != `"-size"` || results[2].Status != 400 || results[2].Err == "" {
		t.Fatalf(`"-size" candidate = %+v`, results[2])
	}
	for order, n := range seen {
		if n != 1 {
			t.Fatalf("order %q requested %d times", order, n)
		}
	}
}

func TestSearchCandidates(t *testing.T) {
	var mu sync.Mutex
	paths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/search":
			_, _ = io.WriteString(w, `{"files":[{"id":"s1","name":"hit"}]}`)
		case "/drive/v1/files:search":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found","error_code":404}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_filter","error_code":1}`)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	results := c.SearchCandidates(context.Background(), "hello")
	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
	if results[0].Status != 400 || results[0].LooksLikeFileList {
		t.Fatalf("candidate 0 = %+v", results[0])
	}
	if results[2].Status != 200 || !results[2].LooksLikeFileList || results[2].Path != "/drive/v1/search" {
		t.Fatalf("candidate 2 = %+v", results[2])
	}
	if results[3].Status != 404 || results[3].LooksLikeFileList {
		t.Fatalf("candidate 3 = %+v", results[3])
	}
	for _, r := range results {
		if len(r.BodyPrefix) > 200 {
			t.Fatalf("body prefix too long: %d", len(r.BodyPrefix))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 4 {
		t.Fatalf("requests = %v", paths)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"file_space_not_enough","error_code":8,"error_description":"no space"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	_, err := c.ListFiles(context.Background(), "", ListOptions{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != 8 || apiErr.Kind != "file_space_not_enough" || apiErr.Status != 400 {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestJSONEnvelopeErrorOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"file_duplicated_name","error_code":3,"error_description":"exists"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	_, err := c.ListFiles(context.Background(), "", ListOptions{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 3 {
		t.Fatalf("err = %v, want APIError code 3", err)
	}
}

func TestNoSessionFails(t *testing.T) {
	c := NewClient(ClientOptions{BaseDrive: "http://127.0.0.1:1"})
	_, err := c.ListFiles(context.Background(), "", ListOptions{})
	if !errors.Is(err, ErrNotBound) {
		t.Fatalf("err = %v, want ErrNotBound", err)
	}
}

func TestSizesAreParsed(t *testing.T) {
	var f File
	if err := json.Unmarshal([]byte(`{"size":"1099511627776"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Size != 1099511627776 {
		t.Fatalf("size = %d", f.Size)
	}
}
