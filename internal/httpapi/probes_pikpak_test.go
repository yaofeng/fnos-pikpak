package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

func TestPikPakProbesSkipWhenUnbound(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)

	results := reg.Run(probe.WithUID(context.Background(), 1000))
	if len(results) != 8 {
		t.Fatalf("results = %d, want 8", len(results))
	}
	for _, r := range results {
		if r.Status != probe.Skip {
			t.Fatalf("%s status = %s (%s), want skip", r.Name, r.Status, r.Detail)
		}
	}
}

func TestPikPakProbesSkipWithoutUID(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)

	results := reg.Run(context.Background())
	for _, r := range results {
		if r.Status != probe.Skip {
			t.Fatalf("%s status = %s, want skip", r.Name, r.Status)
		}
	}
}

func TestSearchProbeWarnsWhenNothingFilters(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/search", "/drive/v1/files:search":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found"}`)
		case "/drive/v1/files":
			if q := r.URL.Query().Get("filters"); q != "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":"file_invalid_file_filters"}`)
				return
			}
			if q := r.URL.Query().Get("q"); q != "" {
				mu.Lock()
				queries = append(queries, q)
				mu.Unlock()
			}
			// q is ignored: the listing comes back unfiltered.
			_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"movie.mkv","kind":"drive#file","size":"42"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newProbeClient(t, srv.URL)
	res := probeSearch(context.Background(), c)

	if res.Status != probe.Warn {
		t.Fatalf("status = %s (%s), want warn: a 200 that ignores q is not a search", res.Status, res.Detail)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) == 0 {
		t.Fatal("no q request was made")
	}
	// The probe searches for a name no drive can contain, so a filtering
	// endpoint answers with an empty list instead of the folder contents.
	if queries[0] != pikpak.SearchProbeQuery {
		t.Fatalf("query = %q, want %q", queries[0], pikpak.SearchProbeQuery)
	}
}

func TestOrderProbeReportsEveryCandidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_argument","error_code":3}`)
	}))
	defer srv.Close()

	c := newProbeClient(t, srv.URL)
	res := probeOrder(context.Background(), c)

	if res.Status != probe.Warn {
		t.Fatalf("status = %s, want warn", res.Status)
	}
	results, ok := res.Data["results"].([]pikpak.OrderResult)
	if !ok || len(results) == 0 {
		t.Fatalf("results = %#v", res.Data["results"])
	}
	if !strings.Contains(res.Detail, fmt.Sprintf("%d", len(results))) {
		t.Fatalf("detail %q does not mention all %d candidates", res.Detail, len(results))
	}
}

func TestOfflineProbeCreatesPollsAndCleansUp(t *testing.T) {
	var mu sync.Mutex
	var deleted bool
	var gotCreate map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			_ = json.NewDecoder(r.Body).Decode(&gotCreate)
			_, _ = io.WriteString(w, `{"task":{"id":"t1","type":"offline","phase":"PHASE_TYPE_PENDING",
				"file_id":"f1","file_name":"index.html","file_size":"12"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/tasks/t1":
			_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_COMPLETE","progress":100,
				"file_id":"f1","file_name":"index.html","file_size":"12"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/drive/v1/tasks":
			mu.Lock()
			deleted = r.URL.Query().Get("delete_files") == "true"
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := newProbeClient(t, srv.URL)
	res := probeOffline(context.Background(), c)

	if res.Status != probe.Pass {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
	if gotCreate["upload_type"] != "UPLOAD_TYPE_URL" {
		t.Fatalf("create body = %v", gotCreate)
	}
	mu.Lock()
	defer mu.Unlock()
	if !deleted {
		t.Fatal("the probe left its offline task behind")
	}
}

func TestOfflineProbeFailsWhenCreateIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_argument","error_code":3,"error_description":"bad url"}`)
	}))
	defer srv.Close()

	res := probeOffline(context.Background(), newProbeClient(t, srv.URL))
	if res.Status != probe.Fail {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "invalid_argument") {
		t.Fatalf("detail = %q", res.Detail)
	}
}

func TestPagesProbePassesWhenWalkEnds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"files":[{"id":"f1"},{"id":"f2"}]}`)
	}))
	defer srv.Close()

	res := probePages(context.Background(), newProbeClient(t, srv.URL))
	if res.Status != probe.Pass {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
}

func TestPagesProbeFailsOnTokenLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"files":[{"id":"f1"}],"next_page_token":"same"}`)
	}))
	defer srv.Close()

	res := probePages(context.Background(), newProbeClient(t, srv.URL))
	if res.Status != probe.Fail {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
	if !strings.Contains(res.Detail, "循环") {
		t.Fatalf("detail = %q", res.Detail)
	}
}

func TestBurstProbePassesWhenAllSucceed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"files":[]}`)
	}))
	defer srv.Close()

	res := probeBurst(context.Background(), newProbeClient(t, srv.URL))
	if res.Status != probe.Pass {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
}

func TestOfflineProbeFallsBackToTheNextCandidate(t *testing.T) {
	var mu sync.Mutex
	var posts int
	var deleted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			mu.Lock()
			posts++
			n := posts
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"task_support_save_snapshot","error_code":3}`)
				return
			}
			_, _ = io.WriteString(w, `{"task":{"id":"t2","phase":"PHASE_TYPE_PENDING","file_id":"f2"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/tasks/t2":
			_, _ = io.WriteString(w, `{"id":"t2","phase":"PHASE_TYPE_COMPLETE","progress":100}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/drive/v1/tasks":
			mu.Lock()
			deleted++
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	res := probeOffline(context.Background(), newProbeClient(t, srv.URL))
	if res.Status != probe.Pass {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
	if posts < 2 {
		t.Fatalf("posts = %d, want a retry with another URL", posts)
	}
	mu.Lock()
	defer mu.Unlock()
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the created task)", deleted)
	}
}

func TestFreshProbeFileIsUniqueAndUnchangedByGCID(t *testing.T) {
	t.Setenv("TRIM_PKGTMP", t.TempDir())

	first, err := ensureFreshProbeFile()
	if err != nil {
		t.Fatalf("ensureFreshProbeFile: %v", err)
	}
	second, err := ensureFreshProbeFile()
	if err != nil {
		t.Fatalf("ensureFreshProbeFile: %v", err)
	}
	if first == second {
		t.Fatalf("both runs wrote %s; a fresh upload needs a new name", first)
	}
	st, err := os.Stat(first)
	if err != nil || st.Size() != uploadProbeSize {
		t.Fatalf("size = %v (err %v), want %d", st, err, uploadProbeSize)
	}
	g1, err := pikpak.GCIDFile(first)
	if err != nil {
		t.Fatalf("GCIDFile: %v", err)
	}
	g2, err := pikpak.GCIDFile(second)
	if err != nil {
		t.Fatalf("GCIDFile: %v", err)
	}
	if g1 == g2 {
		t.Fatal("both fresh files share a GCID, so PikPak would answer with an instant upload")
	}
}

// newProbeClient builds a client that talks to url with a static token, for
// exercising the probe functions without a session store.
func newProbeClient(t *testing.T, url string) *pikpak.Client {
	t.Helper()
	return pikpak.NewClient(pikpak.ClientOptions{
		BaseDrive: url, BaseUser: url, Session: staticToken("tok"), DeviceID: "dev",
	})
}

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func TestPikPakProbesRunWhenBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{"task":{"id":"t1","phase":"PHASE_TYPE_PENDING","file_id":"f1","file_size":"12"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/tasks/t1":
			_, _ = io.WriteString(w, `{"id":"t1","phase":"PHASE_TYPE_COMPLETE","progress":100,"file_id":"f1","file_size":"12"}`)
		case r.Method == http.MethodDelete && r.URL.Path == "/drive/v1/tasks":
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/drive/v1/about":
			_, _ = io.WriteString(w, `{"quota":{"limit":"1000","usage":"250"},"expires_at":""}`)
		case r.URL.Path == "/drive/v1/privilege/vip":
			_, _ = io.WriteString(w, `{"data":{"type":"platinum","expire":"2027-01-01T00:00:00Z"}}`)
		case r.URL.Path == "/drive/v1/search":
			// A filtering endpoint returns nothing for the probe query.
			_, _ = io.WriteString(w, `{"files":[]}`)
		case r.URL.Path == "/drive/v1/files:search":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found"}`)
		case r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"movie.mkv","kind":"drive#file","size":"42"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	ctx := context.Background()
	if err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at", RefreshToken: "rt",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)
	results := reg.Run(probe.WithUID(ctx, 1000))

	byName := map[string]probe.Result{}
	for _, r := range results {
		byName[r.Name] = r
	}
	for name, want := range map[string]probe.Status{
		"pikpak.list": probe.Pass, "pikpak.quota": probe.Pass,
		"pikpak.vip": probe.Pass, "pikpak.order": probe.Pass, "pikpak.search": probe.Pass,
		"pikpak.pages": probe.Pass, "pikpak.burst": probe.Pass, "pikpak.offline": probe.Pass,
	} {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("probe %s missing", name)
		}
		if r.Status != want {
			t.Fatalf("%s status = %s (%s), want %s", name, r.Status, r.Detail, want)
		}
	}

	// Quota numbers must survive the string-typed JSON.
	var quota struct {
		Limit int64 `json:"quota_limit"`
		Usage int64 `json:"quota_usage"`
	}
	raw, _ := json.Marshal(byName["pikpak.quota"].Data)
	_ = json.Unmarshal(raw, &quota)
	if quota.Limit != 1000 || quota.Usage != 250 {
		t.Fatalf("quota data = %s", raw)
	}

	// List data carries the first page for the report.
	if byName["pikpak.list"].Data["items"] == nil {
		t.Fatalf("list data = %v", byName["pikpak.list"].Data)
	}
}
