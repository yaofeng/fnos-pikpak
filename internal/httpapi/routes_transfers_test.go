package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"fnos-pikpak/internal/store"
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
	bindTestUser(t, st, 1000)
	return transferEnv{deps: deps, st: st, upstream: srv}
}

func (e transferEnv) close() { e.upstream.Close() }

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
