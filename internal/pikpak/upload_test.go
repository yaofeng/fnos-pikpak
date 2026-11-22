package pikpak

import (
	"bytes"
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

func TestRequestUploadBody(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"file":{"id":"f1","name":"x.mp4","size":"3","phase":"PHASE_TYPE_COMPLETE"},"resumable":null}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	ticket, err := c.RequestUpload(t.Context(), UploadRequest{
		Name: "x.mp4", Size: 3, GCID: "0d3ced9bec10a777aec23ccc353a8c08a633045e", ParentID: "p1",
	})
	if err != nil {
		t.Fatalf("RequestUpload: %v", err)
	}

	if got["kind"] != "drive#file" || got["name"] != "x.mp4" || got["size"] != float64(3) {
		t.Fatalf("body = %v", got)
	}
	if got["hash"] != "0D3CED9BEC10A777AEC23CCC353A8C08A633045E" {
		t.Fatalf("hash = %v (must be upper-case)", got["hash"])
	}
	if got["upload_type"] != "UPLOAD_TYPE_RESUMABLE" || got["folder_type"] != "NORMAL" || got["parent_id"] != "p1" {
		t.Fatalf("body = %v", got)
	}
	provider, _ := got["objProvider"].(map[string]any)
	if provider["provider"] != "UPLOAD_TYPE_UNKNOWN" {
		t.Fatalf("objProvider = %v", got["objProvider"])
	}

	if ticket.Resumable != nil {
		t.Fatalf("instant upload should have nil resumable: %+v", ticket.Resumable)
	}
	if ticket.File == nil || ticket.File.ID != "f1" || ticket.File.Size != 3 {
		t.Fatalf("file = %+v", ticket.File)
	}
}

func TestRequestUploadZeroSize(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = io.WriteString(w, `{"file":{"id":"f0","size":"0"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	hash, err := GCID(bytes.NewReader(nil), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.RequestUpload(t.Context(), UploadRequest{Name: "empty", Size: 0, GCID: hash}); err != nil {
		t.Fatalf("RequestUpload: %v", err)
	}
	if got["size"] != float64(0) || got["hash"] != strings.ToUpper(hash) || got["hash"] == "" {
		t.Fatalf("body = %v", got)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("must not read") }

func TestCompleteUploadInstantReturnsWithoutReading(t *testing.T) {
	c := NewClient(ClientOptions{BaseDrive: "http://127.0.0.1:1"})
	err := c.CompleteUpload(t.Context(), UploadTicket{File: &File{ID: "f1"}}, errorReader{})
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
}

func TestCompleteUploadUnsupportedProvider(t *testing.T) {
	c := NewClient(ClientOptions{})
	err := c.CompleteUpload(t.Context(), UploadTicket{
		File: &File{ID: "f1", Size: 1},
		Resumable: &Resumable{
			Provider: "PROVIDER_S3",
			Params:   map[string]string{"bucket": "b", "secret": "s"},
		},
	}, bytes.NewReader([]byte("x")))

	if !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("err = %v, want ErrUnsupportedProvider", err)
	}
	var upe *UnsupportedProviderError
	if !errors.As(err, &upe) {
		t.Fatalf("err type = %T", err)
	}
	if upe.Provider != "PROVIDER_S3" || len(upe.ParamKeys) != 2 {
		t.Fatalf("upe = %+v", upe)
	}
	for _, k := range upe.ParamKeys {
		if k == "s" || k == "b" {
			t.Fatalf("param keys leaked values: %v", upe.ParamKeys)
		}
	}
}

func TestNormalizeOSSEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://vip-lixian-07.mypikpak.net":   "https://mypikpak.net",
		"vip-lixian-07.mypikpak.net":           "https://mypikpak.net",
		"https://oss-cn-hangzhou.aliyuncs.com": "https://oss-cn-hangzhou.aliyuncs.com",
		"oss-cn-hangzhou.aliyuncs.com":         "https://oss-cn-hangzhou.aliyuncs.com",
	}
	for in, want := range cases {
		got, err := normalizeOSSEndpoint(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Fatalf("%s -> %s, want %s", in, got, want)
		}
	}
}

// fakeOSS serves the minimal OSS surface CompleteUpload uses.
type fakeOSS struct {
	mu        sync.Mutex
	puts      []int
	parts     []int
	uploads   int
	completes int
	aborts    int
	// failFirstPart makes the first part upload fail once, to exercise retry.
	failFirstPart bool
	partAttempts  int
	tokens        []string
}

func (f *fakeOSS) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokens = append(f.tokens, r.Header.Get("X-OSS-Security-Token"))
		f.mu.Unlock()

		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			f.mu.Lock()
			f.uploads++
			f.mu.Unlock()
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult><Bucket>bkt</Bucket><Key>k</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodDelete && q.Get("uploadId") != "":
			f.mu.Lock()
			f.aborts++
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && q.Get("partNumber") != "":
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.partAttempts++
			fail := f.failFirstPart && f.partAttempts == 1
			f.parts = append(f.parts, len(body))
			f.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("ETag", `"etag-`+q.Get("partNumber")+`"`)
		case r.Method == http.MethodPost && q.Get("uploadId") != "":
			f.mu.Lock()
			f.completes++
			f.mu.Unlock()
			// Malformed XML with HTTP 200: the upload still succeeded.
			_, _ = io.WriteString(w, `<?xml version="1.0"?><CompleteMultipartUploadResult><ETag>abc`)
		case r.Method == http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.puts = append(f.puts, len(body))
			f.mu.Unlock()
		default:
			t.Errorf("unexpected OSS request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
}

func ossTicket(endpoint string, size int64) UploadTicket {
	return UploadTicket{
		File: &File{ID: "f1", Size: size},
		Resumable: &Resumable{
			Provider: "PROVIDER_ALIYUN",
			Params: map[string]string{
				"access_key_id":     "id",
				"access_key_secret": "secret",
				"bucket":            "bkt",
				"endpoint":          endpoint,
				"key":               "k",
				"security_token":    "sts-token",
			},
		},
	}
}

func TestCompleteUploadMultipart(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	const size = 13 << 20
	c := NewClient(ClientOptions{})
	err := c.CompleteUpload(t.Context(), ossTicket(srv.URL, size), io.LimitReader(zeroReader{}, size))
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}

	if fake.uploads != 1 || fake.completes != 1 {
		t.Fatalf("uploads=%d completes=%d", fake.uploads, fake.completes)
	}
	if len(fake.parts) != 3 {
		t.Fatalf("parts = %v, want 3", fake.parts)
	}
	total := 0
	for _, n := range fake.parts {
		if int64(n) > partSize {
			t.Fatalf("part %d exceeds part size %d", n, partSize)
		}
		total += n
	}
	if total != size {
		t.Fatalf("uploaded %d bytes, want %d", total, size)
	}
	for _, tok := range fake.tokens {
		if tok != "sts-token" {
			t.Fatalf("security token = %q", tok)
		}
	}
}

func TestCompleteUploadSmallFilePutsObject(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	const size = 1 << 20
	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(t.Context(), ossTicket(srv.URL, size), io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if len(fake.puts) != 1 || fake.puts[0] != size {
		t.Fatalf("puts = %v", fake.puts)
	}
	if fake.uploads != 0 {
		t.Fatalf("uploads = %d, want 0", fake.uploads)
	}
}

func TestCompleteUploadEmptyFilePutsObject(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(t.Context(), ossTicket(srv.URL, 0), bytes.NewReader(nil)); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if len(fake.puts) != 1 || fake.puts[0] != 0 {
		t.Fatalf("puts = %v", fake.puts)
	}
}

func TestWaitFileComplete(t *testing.T) {
	old := uploadPollInterval
	uploadPollInterval = time.Millisecond
	t.Cleanup(func() { uploadPollInterval = old })

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/files/f1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			_, _ = io.WriteString(w, `{"id":"f1","name":"x","phase":"PHASE_TYPE_PENDING","size":"1"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"f1","name":"x","phase":"PHASE_TYPE_COMPLETE","size":"1"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	f, err := c.WaitFileComplete(t.Context(), "f1", 2*time.Second)
	if err != nil {
		t.Fatalf("WaitFileComplete: %v", err)
	}
	if f.Phase != "PHASE_TYPE_COMPLETE" || calls != 3 {
		t.Fatalf("phase=%s calls=%d", f.Phase, calls)
	}
}

func TestWaitFileCompleteTimesOut(t *testing.T) {
	old := uploadPollInterval
	uploadPollInterval = time.Millisecond
	t.Cleanup(func() { uploadPollInterval = old })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"f1","phase":"PHASE_TYPE_RUNNING"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	if _, err := c.WaitFileComplete(t.Context(), "f1", 20*time.Millisecond); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRequestUploadCarriesRequestedSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"resumable":{"provider":"PROVIDER_ALIYUN","params":{"bucket":"b","endpoint":"https://oss","key":"k"}}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &fakeTokens{token: "t"})
	const size = 13 << 20
	ticket, err := c.RequestUpload(t.Context(), UploadRequest{Name: "big.bin", Size: size, GCID: "x"})
	if err != nil {
		t.Fatalf("RequestUpload: %v", err)
	}
	if ticket.File != nil {
		t.Fatalf("file = %+v, want nil", ticket.File)
	}
	if ticket.Size != size {
		t.Fatalf("ticket.Size = %d, want %d", ticket.Size, size)
	}
}

func TestCompleteUploadUsesRequestedSizeWithoutFile(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	const size = 13 << 20
	ticket := ossTicket(srv.URL, size)
	ticket.File = nil
	ticket.Size = size

	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(t.Context(), ticket, io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if fake.uploads != 1 || len(fake.parts) != 3 {
		t.Fatalf("uploads=%d parts=%v, want multipart", fake.uploads, fake.parts)
	}
}

func TestCompleteUploadStopsWhenCanceled(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	const size = 13 << 20
	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(ctx, ossTicket(srv.URL, size), io.LimitReader(zeroReader{}, size)); err == nil {
		t.Fatal("a canceled upload reported success")
	}
	if fake.completes != 0 {
		t.Fatalf("completes = %d, want 0", fake.completes)
	}
}

func TestCanceledSmallUploadDoesNotPutObject(t *testing.T) {
	fake := &fakeOSS{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(ctx, ossTicket(srv.URL, 5), strings.NewReader("hello")); err == nil {
		t.Fatal("a canceled upload reported success")
	}
	if len(fake.puts) != 0 {
		t.Fatalf("puts = %v, want none", fake.puts)
	}
}

func TestCompleteUploadRetriesAFailedPart(t *testing.T) {
	fake := &fakeOSS{failFirstPart: true}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	const size = 13 << 20
	c := NewClient(ClientOptions{})
	if err := c.CompleteUpload(t.Context(), ossTicket(srv.URL, size), io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatalf("a transient part failure should be retried: %v", err)
	}
	if fake.completes != 1 {
		t.Fatalf("completes = %d", fake.completes)
	}
	if fake.partAttempts < 4 {
		t.Fatalf("part attempts = %d, want the failed one retried", fake.partAttempts)
	}
}
