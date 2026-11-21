package stream

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRange(t *testing.T) {
	cases := []struct {
		header string
		size   int64
		start  int64
		end    int64
		err    error
	}{
		{"bytes=0-1023", 1024, 0, 1023, nil},
		{"bytes=1024-", 4096, 1024, 4095, nil},
		{"bytes=-512", 4096, 3584, 4095, nil},
		{"bytes=5000-6000", 4096, 0, 0, ErrRangeNotSatisfiable},
		{"bytes=abc", 4096, 0, 0, ErrInvalidRange},
		{"", 4096, 0, 4095, nil},
		{"bytes=0-999999", 4096, 0, 4095, nil},
		{"bytes=10-5", 4096, 0, 0, ErrInvalidRange},
		{"bytes=0-1,5-6", 4096, 0, 0, ErrInvalidRange},
	}
	for _, c := range cases {
		start, end, err := ParseRange(c.header, c.size)
		if !errors.Is(err, c.err) {
			t.Errorf("%q: err = %v, want %v", c.header, err, c.err)
			continue
		}
		if err == nil && (start != c.start || end != c.end) {
			t.Errorf("%q: got %d-%d, want %d-%d", c.header, start, end, c.start, c.end)
		}
	}
}

func writeTestFile(t *testing.T, size int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeFileRange(t *testing.T) {
	path := writeTestFile(t, 4096)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Range", "bytes=100-199")
	rec := httptest.NewRecorder()
	ServeFile(rec, req, path, 4096)

	res := rec.Result()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Range"); got != "bytes 100-199/4096" {
		t.Fatalf("content-range = %q", got)
	}
	if res.ContentLength != 100 {
		t.Fatalf("length = %d", res.ContentLength)
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 100 || body[0] != byte(100%251) {
		t.Fatalf("body len=%d first=%d", len(body), body[0])
	}
}

func TestServeFileUnsatisfiable(t *testing.T) {
	path := writeTestFile(t, 4096)

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Range", "bytes=5000-6000")
	rec := httptest.NewRecorder()
	ServeFile(rec, req, path, 4096)

	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes */4096" {
		t.Fatalf("content-range = %q", got)
	}
}

func TestServeFileHeadHasNoBody(t *testing.T) {
	path := writeTestFile(t, 4096)

	req := httptest.NewRequest(http.MethodHead, "/x", nil)
	rec := httptest.NewRecorder()
	ServeFile(rec, req, path, 4096)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %d bytes", rec.Body.Len())
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("accept-ranges = %q", rec.Header().Get("Accept-Ranges"))
	}
}
