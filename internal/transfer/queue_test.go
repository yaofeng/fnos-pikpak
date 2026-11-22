package transfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newQueue(t *testing.T) (*Queue, *store.Store, context.CancelFunc) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{Store: st, MaxGlobal: 1, Tick: 20 * time.Millisecond}
	q.Start(ctx)
	t.Cleanup(func() {
		cancel()
		q.Wait()
	})
	return q, st, cancel
}

func queuedDownload(t *testing.T, st *store.Store, id string, size int64) store.DownloadJob {
	t.Helper()
	dir := t.TempDir()
	job := store.DownloadJob{
		ID: id, UID: 1000, FileID: "f-" + id, FileName: id + ".bin",
		DestDir: dir, DestPath: filepath.Join(dir, id+".bin"), Size: size, Status: "queued",
	}
	if err := st.PutDownloadJob(context.Background(), job); err != nil {
		t.Fatalf("put job: %v", err)
	}
	return job
}

func TestQueueDownloadsAndMarksDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()

	q, st, _ := newQueue(t)
	job := queuedDownload(t, st, "j1", 10)
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: staticLink(srv.URL, 10)}, nil
	}
	q.Notify()

	waitFor(t, "download to finish", func() bool {
		got, err := st.GetDownloadJob(context.Background(), 1000, "j1")
		return err == nil && got.Status == "done"
	})
	content, err := os.ReadFile(job.DestPath)
	if err != nil || string(content) != "0123456789" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestQueueSkipsPausedJobs(t *testing.T) {
	var calls int32
	q, st, _ := newQueue(t)
	job := queuedDownload(t, st, "j1", 10)
	job.Status = "paused"
	if err := st.PutDownloadJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	q.NewDownloader = func(int) (*Downloader, error) {
		atomic.AddInt32(&calls, 1)
		return &Downloader{Links: staticLink("http://127.0.0.1:1", 10)}, nil
	}
	q.Notify()
	time.Sleep(150 * time.Millisecond)

	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("a paused job was started")
	}
	if got, _ := st.GetDownloadJob(context.Background(), 1000, "j1"); got.Status != "paused" {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestQueueRespectsGlobalConcurrency(t *testing.T) {
	release := make(chan struct{})
	var started int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&started, 1)
		<-release
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()

	q, st, _ := newQueue(t)
	queuedDownload(t, st, "j1", 10)
	queuedDownload(t, st, "j2", 10)
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: staticLink(srv.URL, 10)}, nil
	}
	q.Notify()

	waitFor(t, "first transfer to start", func() bool { return atomic.LoadInt32(&started) == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&started); got != 1 {
		t.Fatalf("started = %d with MaxGlobal=1", got)
	}

	close(release)
	waitFor(t, "both transfers", func() bool {
		a, _ := st.GetDownloadJob(context.Background(), 1000, "j1")
		b, _ := st.GetDownloadJob(context.Background(), 1000, "j2")
		return a.Status == "done" && b.Status == "done"
	})
}

func TestQueueMarksDownloadFailure(t *testing.T) {
	q, st, _ := newQueue(t)
	queuedDownload(t, st, "j1", 10)
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: LinkSourceFunc(func(context.Context, string) (Link, error) {
			return Link{}, errors.New("upstream blew up")
		})}, nil
	}
	q.Notify()

	waitFor(t, "failure", func() bool {
		got, err := st.GetDownloadJob(context.Background(), 1000, "j1")
		return err == nil && got.Status == "failed"
	})
	got, _ := st.GetDownloadJob(context.Background(), 1000, "j1")
	if got.ErrorCode != "download_failed" || got.ErrorMessage == "" {
		t.Fatalf("job = %+v", got)
	}
}

func TestQueuePauseKeepsPausedStatus(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	q, st, _ := newQueue(t)
	job := queuedDownload(t, st, "j1", 10)
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: staticLink(srv.URL, 10)}, nil
	}
	q.Notify()

	waitFor(t, "running", func() bool {
		got, _ := st.GetDownloadJob(context.Background(), 1000, "j1")
		return got.Status == "running"
	})

	job.Status = "paused"
	if err := st.PutDownloadJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	q.CancelRunning("j1")

	waitFor(t, "paused to settle", func() bool {
		got, _ := st.GetDownloadJob(context.Background(), 1000, "j1")
		return got.Status == "paused"
	})
	time.Sleep(80 * time.Millisecond)
	if got, _ := st.GetDownloadJob(context.Background(), 1000, "j1"); got.Status != "paused" {
		t.Fatalf("worker overwrote the pause: %q", got.Status)
	}
}

func TestQueueUploadsAndAppliesSkipPolicy(t *testing.T) {
	q, st, _ := newQueue(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.mkv")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := store.UploadJob{
		ID: "u1", UID: 1000, LocalPath: path, ParentID: "p1",
		RemoteName: "a.mkv", Size: 5, Status: "queued",
	}
	if err := st.PutUploadJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := st.SetUserSetting(context.Background(), 1000, "conflict", "skip"); err != nil {
		t.Fatal(err)
	}

	q.NewUploader = func(int) (*Uploader, error) {
		return &Uploader{Client: &fakeUploadClient{files: []pikpak.File{
			{ID: "x1", Name: "a.mkv", Kind: "drive#file"},
		}}}, nil
	}
	q.Notify()

	waitFor(t, "upload skip", func() bool {
		got, err := st.GetUploadJob(context.Background(), 1000, "u1")
		return err == nil && got.Status == "skipped"
	})
	if got, _ := st.GetUploadJob(context.Background(), 1000, "u1"); got.ErrorCode != "conflict" {
		t.Fatalf("job = %+v", got)
	}
}

func openQueueStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// hangingServer answers a download request and never sends a body, so the
// worker sits in the transfer until its context is canceled.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestShutdownRequeuesRunningDownloads(t *testing.T) {
	srv := hangingServer(t)
	st := openQueueStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{Store: st, MaxGlobal: 1, Tick: 20 * time.Millisecond}
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: staticLink(srv.URL, 10)}, nil
	}
	q.Start(ctx)
	queuedDownload(t, st, "j1", 10)
	q.Notify()
	waitFor(t, "download to start", func() bool {
		got, err := st.GetDownloadJob(context.Background(), 1000, "j1")
		return err == nil && got.Status == "running"
	})

	// A graceful stop is not a cancel: the job goes back to the queue so the
	// next start resumes it.
	cancel()
	q.Wait()
	got, err := st.GetDownloadJob(context.Background(), 1000, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" {
		t.Fatalf("status after shutdown = %q, want queued", got.Status)
	}
}

func TestDeletedJobIsNotResurrectedByItsWorker(t *testing.T) {
	srv := hangingServer(t)
	st := openQueueStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := &Queue{Store: st, MaxGlobal: 1, Tick: 20 * time.Millisecond}
	q.NewDownloader = func(int) (*Downloader, error) {
		return &Downloader{Links: staticLink(srv.URL, 10)}, nil
	}
	q.Start(ctx)
	queuedDownload(t, st, "j1", 10)
	q.Notify()
	waitFor(t, "download to start", func() bool {
		got, err := st.GetDownloadJob(context.Background(), 1000, "j1")
		return err == nil && got.Status == "running"
	})

	if err := st.DeleteDownloadJob(context.Background(), 1000, "j1"); err != nil {
		t.Fatal(err)
	}
	q.CancelRunning("j1")
	waitFor(t, "worker to release the job", func() bool {
		_, err := st.GetDownloadJob(context.Background(), 1000, "j1")
		return errors.Is(err, store.ErrNotFound)
	})
	time.Sleep(100 * time.Millisecond)
	if _, err := st.GetDownloadJob(context.Background(), 1000, "j1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted job came back: %v", err)
	}
}

// blockingUploadClient parks inside RequestUpload until the worker cancels.
type blockingUploadClient struct{ fakeUploadClient }

func (b *blockingUploadClient) RequestUpload(ctx context.Context, in pikpak.UploadRequest) (pikpak.UploadTicket, error) {
	<-ctx.Done()
	return pikpak.UploadTicket{}, ctx.Err()
}

func TestShutdownRequeuesRunningUploads(t *testing.T) {
	st := openQueueStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{Store: st, MaxGlobal: 1, Tick: 20 * time.Millisecond}
	q.NewUploader = func(int) (*Uploader, error) {
		return &Uploader{Client: &blockingUploadClient{}}, nil
	}
	q.Start(ctx)

	job := uploadJob(t, "a.mkv")
	job.Status = "queued"
	if err := st.PutUploadJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	q.Notify()
	waitFor(t, "upload to start", func() bool {
		got, err := st.GetUploadJob(context.Background(), 1000, job.ID)
		return err == nil && got.Status == "running"
	})

	cancel()
	q.Wait()
	got, err := st.GetUploadJob(context.Background(), 1000, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" {
		t.Fatalf("status after shutdown = %q, want queued", got.Status)
	}
}
