package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/transfer"
)

func allowAll(context.Context, int, string, bool) error { return nil }

type transferEnv struct {
	deps     Deps
	st       *store.Store
	upstream *httptest.Server
}

func newTransferEnv(t *testing.T, guard DirGuard) transferEnv {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && len(r.URL.Path) > len("/drive/v1/files/") &&
			r.URL.Path[:len("/drive/v1/files/")] == "/drive/v1/files/" {
			_, _ = io.WriteString(w, `{"id":"f1","name":"a.mkv","size":"10","kind":"drive#file","phase":"PHASE_TYPE_COMPLETE"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	deps, st := newAuthTestDeps(t, srv.URL)
	deps.DirGuard = guard
	// A real (unstarted) queue keeps the notify/cancel paths honest: the
	// handlers must not assume a nil queue.
	deps.Queue = &transfer.Queue{Store: st}
	bindTestUser(t, st, 1000)
	return transferEnv{deps: deps, st: st, upstream: srv}
}

func (e transferEnv) close() { e.upstream.Close() }

func TestDevDirGuardAllowsAbsolutePathsOnly(t *testing.T) {
	guard := DevDirGuard()
	if err := guard(t.Context(), 1000, "/tmp", true); err != nil {
		t.Fatalf("absolute path rejected: %v", err)
	}
	if err := guard(t.Context(), 1000, "relative/path", true); !errors.Is(err, ErrDirForbidden) {
		t.Fatalf("relative path err = %v", err)
	}
}

func TestCreateDownloadCreatesQueuedJob(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)
	dir := t.TempDir()

	rec, body := postJSON(t, h, "/api/downloads", "1000",
		`{"file_ids":["f1"],"dest_dir":"`+dir+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := env.st.ListDownloadJobs(t.Context(), 1000)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
	job := jobs[0]
	if job.Status != "queued" || job.Size != 10 || job.FileName != "a.mkv" {
		t.Fatalf("job = %+v", job)
	}
	if job.DestPath != filepath.Join(dir, "a.mkv") {
		t.Fatalf("dest = %q", job.DestPath)
	}
	// The response must describe the stored row, timestamps included.
	created, _ := body["created"].([]any)
	first, _ := created[0].(map[string]any)
	if ts, _ := first["created_at"].(string); ts == "" || strings.HasPrefix(ts, "0001-") {
		t.Fatalf("created_at = %q", first["created_at"])
	}
}

func TestCreateDownloadPicksUniqueNameWhenFileExists(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, body := postJSON(t, h, "/api/downloads", "1000",
		`{"file_ids":["f1"],"dest_dir":"`+dir+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, _ := env.st.ListDownloadJobs(t.Context(), 1000)
	if got := filepath.Base(jobs[0].DestPath); got != "a (1).mkv" {
		t.Fatalf("dest name = %q", got)
	}
}

func TestCreateDownloadRejectsForeignDirectory(t *testing.T) {
	env := newTransferEnv(t, func(context.Context, int, string, bool) error { return ErrDirForbidden })
	defer env.close()
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/downloads", "1000",
		`{"file_ids":["f1"],"dest_dir":"/vol1/other"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestCreateDownloadWithoutFnOSIs503(t *testing.T) {
	env := newTransferEnv(t, nil)
	defer env.close()
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/downloads", "1000",
		`{"file_ids":["f1"],"dest_dir":"/vol1/1001/pikpak"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestDownloadControls(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)

	job := store.DownloadJob{
		ID: "j1", UID: 1000, FileID: "f1", FileName: "a.mkv",
		DestDir: "/tmp", DestPath: "/tmp/a.mkv", Status: "queued",
	}
	if err := env.st.PutDownloadJob(t.Context(), job); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ action, want string }{
		{"pause", "paused"},
		{"resume", "queued"},
		{"cancel", "canceled"},
	} {
		rec, body := postJSON(t, h, "/api/downloads/j1/"+tc.action, "1000", `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body=%v", tc.action, rec.Code, body)
		}
		got, _ := env.st.GetDownloadJob(t.Context(), 1000, "j1")
		if got.Status != tc.want {
			t.Fatalf("after %s: %q, want %q", tc.action, got.Status, tc.want)
		}
	}
}

func TestDownloadJobsUseSnakeCaseJSON(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)

	if err := env.st.PutDownloadJob(t.Context(), store.DownloadJob{
		ID: "j1", UID: 1000, FileID: "f1", FileName: "a.mkv",
		DestDir: "/tmp", DestPath: "/tmp/a.mkv", Size: 10, Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	rec, body := getJSON(t, h, "/api/downloads", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body)
	}
	job, _ := items[0].(map[string]any)
	for _, key := range []string{"file_name", "dest_path", "status", "size"} {
		if _, ok := job[key]; !ok {
			t.Fatalf("missing %q in %v", key, job)
		}
	}
	if _, leaked := job["UID"]; leaked {
		t.Fatalf("internal UID leaked: %v", job)
	}
}

func TestTransferJobsAreScopedToUser(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)

	if err := env.st.PutDownloadJob(t.Context(), store.DownloadJob{
		ID: "j1", UID: 1000, FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a", Status: "queued",
	}); err != nil {
		t.Fatal(err)
	}
	rec, _ := postJSON(t, h, "/api/downloads/j1/pause", "1001", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other user status = %d", rec.Code)
	}
}

func TestCreateUploadCreatesQueuedJob(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)
	dir := t.TempDir()
	path := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"local_paths": []string{path}, "parent_id": "p1"})
	rec, body := postJSON(t, h, "/api/uploads", "1000", string(raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := env.st.ListUploadJobs(t.Context(), 1000)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
	if jobs[0].RemoteName != "b.txt" || jobs[0].Size != 5 || jobs[0].ParentID != "p1" {
		t.Fatalf("job = %+v", jobs[0])
	}
}

func TestCreateUploadRejectsMissingFile(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/uploads", "1000",
		`{"local_paths":["/tmp/definitely-not-here-9f3"],"parent_id":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestCreateDownloadUsesDefaultDir(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	dir := t.TempDir()
	if err := env.st.SetUserSetting(t.Context(), 1000, "download_dir", dir); err != nil {
		t.Fatalf("SetUserSetting: %v", err)
	}
	h := NewMux(env.deps)

	// No dest_dir: the file goes to the directory the user configured once.
	rec, body := postJSON(t, h, "/api/downloads", "1000", `{"file_ids":["f1"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := env.st.ListDownloadJobs(t.Context(), 1000)
	if err != nil {
		t.Fatalf("ListDownloadJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].DestDir != dir {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestCreateDownloadWithoutDefaultDirAsksForOne(t *testing.T) {
	env := newTransferEnv(t, allowAll)
	defer env.close()
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/downloads", "1000", `{"file_ids":["f1"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	perr, _ := body["error"].(map[string]any)
	if perr["code"] != "download_dir_unset" {
		t.Fatalf("code = %v", perr["code"])
	}
}

// grantedUnder builds a DirGuard that allows only one directory tree, the way
// FnosDirGuard answers for a user with a single granted folder.
func grantedUnder(allowed string) DirGuard {
	return func(_ context.Context, _ int, dir string, _ bool) error {
		if dir == allowed || strings.HasPrefix(dir, allowed+string(filepath.Separator)) {
			return nil
		}
		return ErrDirForbidden
	}
}

func TestCreateUploadRejectsSymlinkOutsideGrants(t *testing.T) {
	allowed, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := newTransferEnv(t, grantedUnder(allowed))
	defer env.close()
	h := NewMux(env.deps)

	link := filepath.Join(allowed, "innocent.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{"local_paths": []string{link}, "parent_id": ""})
	rec, body := postJSON(t, h, "/api/uploads", "1000", string(raw))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := env.st.ListUploadJobs(t.Context(), 1000)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
}

func TestCreateUploadStoresResolvedSource(t *testing.T) {
	allowed, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(allowed, "real.txt")
	if err := os.WriteFile(real, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "alias.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	env := newTransferEnv(t, grantedUnder(allowed))
	defer env.close()
	h := NewMux(env.deps)

	raw, _ := json.Marshal(map[string]any{"local_paths": []string{link}, "parent_id": ""})
	rec, body := postJSON(t, h, "/api/uploads", "1000", string(raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := env.st.ListUploadJobs(t.Context(), 1000)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
	if jobs[0].LocalPath != real {
		t.Fatalf("local path = %q, want the resolved %q", jobs[0].LocalPath, real)
	}
}

func TestBatchDownloadsWithSameNameGetDistinctPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every file in this batch is called the same thing.
		_, _ = io.WriteString(w, `{"id":"`+filepath.Base(r.URL.Path)+`","name":"same.mkv","size":"10","kind":"drive#file"}`)
	}))
	defer srv.Close()
	deps, st := newAuthTestDeps(t, srv.URL)
	deps.DirGuard = allowAll
	deps.Queue = &transfer.Queue{Store: st}
	bindTestUser(t, st, 1000)
	h := NewMux(deps)
	dir := t.TempDir()

	raw, _ := json.Marshal(map[string]any{"file_ids": []string{"f1", "f2"}, "dest_dir": dir})
	rec, body := postJSON(t, h, "/api/downloads", "1000", string(raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	jobs, err := st.ListDownloadJobs(t.Context(), 1000)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
	if jobs[0].DestPath == jobs[1].DestPath {
		t.Fatalf("two jobs share %q and would overwrite each other", jobs[0].DestPath)
	}
	if jobs[0].FileName != "same.mkv" || jobs[1].FileName != "same.mkv" {
		t.Fatalf("names = %+v", jobs)
	}
}

func TestDirGuardAcceptsACachedGrant(t *testing.T) {
	client := newFnosTestClient(t)
	dir := t.TempDir()
	dbDir := t.TempDir()
	st, err := store.Open(filepath.Join(dbDir, "pikpak.db"), filepath.Join(dbDir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	guard := fnosDirGuard(client, CachedGrants(st))

	// The live fnOS list does not contain dir yet (its sync lags), but the
	// user authorized it and the ACL says yes.
	if err := st.GrantFolder(t.Context(), 1000, dir, ""); err != nil {
		t.Fatal(err)
	}
	if err := guard(t.Context(), 1000, dir, true); err != nil {
		t.Fatalf("cached grant rejected: %v", err)
	}

	if err := st.RevokeFolder(t.Context(), 1000, dir); err != nil {
		t.Fatal(err)
	}
	if err := guard(t.Context(), 1000, dir, true); !errors.Is(err, ErrDirForbidden) {
		t.Fatalf("dir outside every grant accepted: %v", err)
	}
}
