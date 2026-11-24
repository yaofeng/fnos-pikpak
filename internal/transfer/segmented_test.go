package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// patternMod keeps the test payload easy to verify at any offset.
const patternMod = 251

func fillPattern(b []byte, offset int64) {
	for i := range b {
		b[i] = byte((offset + int64(i)) % patternMod)
	}
}

func patternBody(size int64) []byte {
	b := make([]byte, size)
	fillPattern(b, 0)
	return b
}

func assertPattern(t *testing.T, path string, size int64) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if int64(len(got)) != size {
		t.Fatalf("size = %d, want %d", len(got), size)
	}
	want := make([]byte, 64<<10)
	for off := int64(0); off < size; off += int64(len(want)) {
		n := int64(len(want))
		if size-off < n {
			n = size - off
		}
		fillPattern(want[:n], off)
		if string(got[off:off+n]) != string(want[:n]) {
			t.Fatalf("content differs at offset %d", off)
		}
	}
}

func parseByteRange(header string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(header, "bytes="), "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	end := size - 1
	if parts[1] != "" {
		if end, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
			return 0, 0, false
		}
	}
	if start < 0 || end < start || end >= size {
		return 0, 0, false
	}
	return start, end, true
}

func writePattern(w io.Writer, start, end int64) {
	buf := make([]byte, 64<<10)
	for off := start; off <= end; {
		n := int64(len(buf))
		if end-off+1 < n {
			n = end - off + 1
		}
		fillPattern(buf[:n], off)
		if _, err := w.Write(buf[:n]); err != nil {
			return
		}
		off += n
	}
}

// rangeServer serves a deterministic pattern and honors Range, so a test can
// see whether the downloader split the file. It holds each response briefly so
// overlapping requests register in maxConcurrent.
func rangeServer(t *testing.T, size int64, maxConcurrent *int32, onRange func(string)) *httptest.Server {
	t.Helper()
	var active int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)
		for {
			cur := atomic.LoadInt32(maxConcurrent)
			if n <= cur || atomic.CompareAndSwapInt32(maxConcurrent, cur, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)

		header := r.Header.Get("Range")
		if onRange != nil {
			onRange(header)
		}
		if header == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			writePattern(w, 0, size-1)
			return
		}
		start, end, ok := parseByteRange(header, size)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		writePattern(w, start, end)
	}))
}

func TestRunDownloadsLargeFileOverSeveralRanges(t *testing.T) {
	const size = 16 << 20
	var maxConcurrent int32
	srv := rangeServer(t, size, &maxConcurrent, nil)
	defer srv.Close()

	job := testJob(t, size)
	d := &Downloader{Links: staticLink(srv.URL, size)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&maxConcurrent); got < 2 {
		t.Fatalf("max concurrent range requests = %d, want the file split over several connections", got)
	}
	assertPattern(t, job.DestPath, size)
	if _, err := os.Stat(job.DestPath + partSuffix + stateSuffix); err == nil {
		t.Fatal("the resume state file was left behind")
	}
}

func TestRunFallsBackWhenTheServerIgnoresRange(t *testing.T) {
	const size = 10 << 20
	body := patternBody(size)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	job := testJob(t, size)
	d := &Downloader{Links: staticLink(srv.URL, size)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertPattern(t, job.DestPath, size)
	if _, err := os.Stat(job.DestPath + partSuffix + stateSuffix); err == nil {
		t.Fatal("a state file survived a download that never segmented")
	}
}

func TestRunResumesSegmentedDownload(t *testing.T) {
	const size = 16 << 20
	st := newPartState(size)
	// Pretend the first segment finished and the second one is half done.
	st.Segments[0].Done = st.Segments[0].End - st.Segments[0].Start + 1
	st.Segments[1].Done = 2 << 20

	job := testJob(t, size)
	part := job.DestPath + partSuffix
	body := patternBody(size)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(body[:st.Segments[0].Done], 0); err != nil {
		t.Fatal(err)
	}
	second := st.Segments[1]
	if _, err := f.WriteAt(body[second.Start:second.Start+second.Done], second.Start); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part+stateSuffix, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var ranges []string
	var maxConcurrent int32
	srv := rangeServer(t, size, &maxConcurrent, func(header string) {
		mu.Lock()
		ranges = append(ranges, header)
		mu.Unlock()
	})
	defer srv.Close()

	d := &Downloader{Links: staticLink(srv.URL, size)}
	if err := d.Run(context.Background(), &job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertPattern(t, job.DestPath, size)

	mu.Lock()
	defer mu.Unlock()
	for _, header := range ranges {
		if strings.HasPrefix(header, "bytes=0-") {
			t.Fatalf("resume asked for bytes already on disk: %q", header)
		}
	}
	want := fmt.Sprintf("bytes=%d-", second.Start+second.Done)
	for _, header := range ranges {
		if strings.HasPrefix(header, want) {
			return
		}
	}
	t.Fatalf("no request resumed the half-done segment at %q; saw %v", want, ranges)
}
