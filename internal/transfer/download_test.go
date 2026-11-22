package transfer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fnos-pikpak/internal/store"
)

func testJob(t *testing.T, size int64) store.DownloadJob {
	t.Helper()
	dir := t.TempDir()
	return store.DownloadJob{
		ID: "j1", UID: 1000, FileID: "f1", FileName: "movie.bin",
		DestDir: dir, DestPath: filepath.Join(dir, "movie.bin"), Size: size, Status: "running",
	}
}

func staticLink(url string, size int64) LinkSourceFunc {
	return func(context.Context, string) (Link, error) { return Link{URL: url, Size: size}, nil }
}

func TestRunDownloadsFreshFile(t *testing.T) {
	var sawRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()

	job := testJob(t, 10)
	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sawRange != "" {
		t.Fatalf("fresh download sent Range %q", sawRange)
	}
	got, _ := os.ReadFile(job.DestPath)
	if string(got) != "0123456789" {
		t.Fatalf("content = %q", got)
	}
	if _, err := os.Stat(job.DestPath + ".pikpak-part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file still exists: %v", err)
	}
}

func TestRunResumesFromPartFile(t *testing.T) {
	var sawRange, sawContentRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRange = r.Header.Get("Range")
		sawContentRange = r.Header.Get("Content-Range")
		w.Header().Set("Content-Range", "bytes 4-9/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "456789")
	}))
	defer srv.Close()

	job := testJob(t, 10)
	if err := os.WriteFile(job.DestPath+".pikpak-part", []byte("0123"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sawRange != "bytes=4-" {
		t.Fatalf("Range = %q, want bytes=4-", sawRange)
	}
	if sawContentRange != "" {
		t.Fatalf("request should not send Content-Range, got %q", sawContentRange)
	}
	got, _ := os.ReadFile(job.DestPath)
	if string(got) != "0123456789" {
		t.Fatalf("content = %q", got)
	}
}

func TestRunRestartsWhenServerIgnoresRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 with the full body: the Range header was ignored.
		_, _ = io.WriteString(w, "abcdefghij")
	}))
	defer srv.Close()

	job := testJob(t, 10)
	if err := os.WriteFile(job.DestPath+".pikpak-part", []byte("XXXX"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(job.DestPath)
	if string(got) != "abcdefghij" {
		t.Fatalf("content = %q, want a full restart", got)
	}
}

func TestRunKeepsPartFileWhenStreamBreaks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = io.WriteString(w, "abcde")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, _ := w.(http.Hijacker)
		if c, _, err := hj.Hijack(); err == nil {
			_ = c.Close()
		}
	}))
	defer srv.Close()

	job := testJob(t, 10)
	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err == nil {
		t.Fatal("a truncated body was reported as success")
	}
	if _, err := os.Stat(job.DestPath + ".pikpak-part"); err != nil {
		t.Fatalf("part file was removed after a recoverable failure: %v", err)
	}
	if _, err := os.Stat(job.DestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final file exists after failure: %v", err)
	}
}

func TestRunRefreshesExpiredLink(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch atomic.AddInt32(&hits, 1) {
		case 1:
			w.WriteHeader(http.StatusForbidden)
		case 2:
			w.WriteHeader(http.StatusGone)
		default:
			_, _ = io.WriteString(w, "ok!")
		}
	}))
	defer srv.Close()

	var fetches int32
	links := LinkSourceFunc(func(context.Context, string) (Link, error) {
		atomic.AddInt32(&fetches, 1)
		return Link{URL: srv.URL, Size: 3}, nil
	})

	job := testJob(t, 3)
	d := &Downloader{Links: links}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&fetches); got < 2 {
		t.Fatalf("link fetches = %d, want a refresh after 403", got)
	}
	got, _ := os.ReadFile(job.DestPath)
	if string(got) != "ok!" {
		t.Fatalf("content = %q", got)
	}
}

func TestRunRejectsSizeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "abc")
	}))
	defer srv.Close()

	job := testJob(t, 10)
	d := &Downloader{Links: staticLink(srv.URL, 10)}
	err := d.Run(context.Background(), &job)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("err = %v, want ErrSizeMismatch", err)
	}
	if _, err := os.Stat(job.DestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a short file was renamed into place")
	}
}

func TestRunRejectsWhenDiskIsFull(t *testing.T) {
	old := freeBytes
	freeBytes = func(string) (int64, error) { return 100, nil }
	t.Cleanup(func() { freeBytes = old })

	job := testJob(t, 1000)
	d := &Downloader{Links: staticLink("http://127.0.0.1:1", 1000)}
	err := d.Run(context.Background(), &job)
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("err = %v, want ErrInsufficientSpace", err)
	}
}

func TestRunReportsProgress(t *testing.T) {
	payload := strings.Repeat("x", 10<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	job := testJob(t, int64(len(payload)))
	var reports int32
	d := &Downloader{
		Links:      staticLink(srv.URL, int64(len(payload))),
		OnProgress: func(int64) { atomic.AddInt32(&reports, 1) },
	}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if atomic.LoadInt32(&reports) == 0 {
		t.Fatal("no progress was reported for a 10 MiB transfer")
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	job := testJob(t, 10)
	d := &Downloader{Links: staticLink(srv.URL, 10)}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err := d.Run(ctx, &job)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunRefusesSymlinkedPartFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()

	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := testJob(t, 10)
	part := job.DestPath + partSuffix
	if err := os.Symlink(outside, part); err != nil {
		t.Fatal(err)
	}

	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err == nil {
		t.Fatal("Run followed a symlinked part file")
	}
	if got, _ := os.ReadFile(outside); string(got) != "keep me" {
		t.Fatalf("outside file was written through the symlink: %q", got)
	}
}

func TestRunRefusesDirectoryTheGuardRejects(t *testing.T) {
	job := testJob(t, 10)
	d := &Downloader{
		Links:    staticLink("http://127.0.0.1:1", 10),
		CheckDir: func(string) error { return ErrDirRejected },
	}
	err := d.Run(context.Background(), &job)
	if !errors.Is(err, ErrDirRejected) {
		t.Fatalf("err = %v, want the guard's refusal", err)
	}
	if _, err := os.Stat(job.DestPath + partSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a part file was created despite the refusal")
	}
}

func TestRunReplacesSymlinkAtDestinationWithoutFollowingIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	defer srv.Close()

	job := testJob(t, 10)
	target := filepath.Join(t.TempDir(), "not-created")
	if err := os.Symlink(target, job.DestPath); err != nil {
		t.Fatal(err)
	}

	d := &Downloader{Links: staticLink(srv.URL, 10)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Rename replaces the link itself; the bytes never land on its target.
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the symlink target was written")
	}
	if got, _ := os.ReadFile(job.DestPath); string(got) != "0123456789" {
		t.Fatalf("content = %q", got)
	}
}
