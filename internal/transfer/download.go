package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fnos-pikpak/internal/store"
)

var (
	ErrSizeMismatch      = errors.New("transfer: downloaded size does not match the file")
	ErrInsufficientSpace = errors.New("transfer: not enough free space")
	ErrLinkExpired       = errors.New("transfer: download link expired")
	// ErrDirRejected is a directory the guard will not accept; the worker
	// re-checks before touching the disk so a folder swapped after the job was
	// accepted cannot redirect the write.
	ErrDirRejected = errors.New("transfer: destination directory is not allowed")
	// ErrUnsafePath means a symlink sits where a plain file must be.
	ErrUnsafePath = errors.New("transfer: refusing to follow a symlink at the destination")
)

const (
	partSuffix     = ".pikpak-part"
	copyBufferSize = 4 << 20
	reportBytes    = 4 << 20
	reportInterval = 2 * time.Second
	maxLinkFetches = 3

	// A large file is fetched over several connections: PikPak's CDN throttles
	// a single stream, and the official clients download the same file over
	// many ranges. Small files stay on one stream, where splitting costs more
	// than it gains.
	segmentThreshold = 8 << 20
	minSegmentSize   = 4 << 20
	maxSegments      = 8
	segmentBuffer    = 256 << 10
	stateSuffix      = ".state"
)

// partState is the resume map for a segmented download. The part file is
// sparse and may hold any set of byte ranges, so a prefix length cannot
// describe it; this sidecar does.
type partState struct {
	Size     int64          `json:"size"`
	Segments []segmentState `json:"segments"`
}

// segmentState covers Start..End inclusive, with Done bytes present from Start.
type segmentState struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

// newPartState splits size into as few segments as minSegmentSize allows, up to
// maxSegments; the last one absorbs the remainder.
func newPartState(size int64) partState {
	n := int((size + minSegmentSize - 1) / minSegmentSize)
	if n > maxSegments {
		n = maxSegments
	}
	if n < 1 {
		n = 1
	}
	chunk := size / int64(n)
	st := partState{Size: size}
	for i := 0; i < n; i++ {
		start := int64(i) * chunk
		end := start + chunk - 1
		if i == n-1 {
			end = size - 1
		}
		st.Segments = append(st.Segments, segmentState{Start: start, End: end})
	}
	return st
}

// Link is a fresh signed download URL.
type Link struct {
	URL  string
	Size int64
}

// LinkSource fetches a download URL for a PikPak file id.
type LinkSource interface {
	DownloadLink(ctx context.Context, fileID string) (Link, error)
}

// Downloader streams one file to disk with Range resume. It is stateless: the
// queue owns jobs and persistence.
type Downloader struct {
	Links LinkSource
	HTTP  *http.Client
	// OnProgress is called while bytes arrive, throttled to roughly every
	// 4 MiB or 2 seconds.
	OnProgress func(downloaded int64)
	// CheckDir re-validates the destination directory at write time. The job
	// was checked when it was created; this catches a folder swapped for a
	// symlink (or a grant revoked) in between.
	CheckDir func(dir string) error
}

func (d *Downloader) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return http.DefaultClient
}

// freeBytes is a variable so tests can simulate a full disk.
var freeBytes = statfsFree

func statfsFree(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(uint64(st.Bavail) * uint64(st.Bsize)), nil
}

func (d *Downloader) Run(ctx context.Context, job *store.DownloadJob) error {
	if d.CheckDir != nil {
		if err := d.CheckDir(filepath.Dir(job.DestPath)); err != nil {
			return ErrDirRejected
		}
	}
	if job.Size > 0 {
		free, err := freeBytes(filepath.Dir(job.DestPath))
		if err != nil {
			return err
		}
		if needed := job.Size + job.Size/20; free < needed {
			return ErrInsufficientSpace
		}
	}

	part := job.DestPath + partSuffix
	offset := int64(0)
	// Lstat, not Stat: a symlink at the part path would otherwise send the
	// write wherever it points.
	if st, err := os.Lstat(part); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		offset = st.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if job.Size > 0 && offset > job.Size {
		// A stale part file larger than the file cannot be resumed.
		if err := os.Remove(part); err != nil {
			return err
		}
		offset = 0
	}
	// The sidecar decides how a large part file continues: a sparse file whose
	// size already equals the file may still be missing most of its bytes, so
	// this check has to come before the "part is complete" shortcut.
	if job.Size >= segmentThreshold && stateExists(part) {
		if st, ok := loadPartState(part, job.Size); ok {
			return d.runSegmented(ctx, job, part, st)
		}
		// A sidecar that does not describe this file leaves nothing to resume:
		// the bytes on disk cannot be attributed to ranges.
		if err := os.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removeState(part)
		offset = 0
	}
	if job.Size > 0 && offset == job.Size {
		return finalize(part, job)
	}
	if job.Size >= segmentThreshold && offset == 0 {
		return d.runSegmented(ctx, job, part, newPartState(job.Size))
	}
	return d.streamWhole(ctx, job, part, offset)
}

// streamWhole downloads the file over one connection, resuming a prefix part
// file. It is also the fallback for a server that ignores Range.
func (d *Downloader) streamWhole(ctx context.Context, job *store.DownloadJob, part string, offset int64) error {
	resp, link, err := d.open(ctx, job.FileID, offset)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if job.Size == 0 && link.Size > 0 {
		job.Size = link.Size
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if resp.StatusCode == http.StatusOK && offset > 0 {
		// The server ignored Range; the bytes after offset are a full body
		// starting at zero, so the existing prefix must go.
		if err := f.Truncate(0); err != nil {
			return err
		}
		offset = 0
		job.Downloaded = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	written, err := d.copyBody(ctx, f, resp.Body, offset)
	job.Downloaded = written
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return finalize(part, job)
}

// errRangeUnsupported means the server answered a ranged request with the
// whole file; the download falls back to a single stream.
var errRangeUnsupported = errors.New("transfer: server ignored the range request")

// runSegmented fetches the file over several connections, each covering one
// range from the sidecar. It resumes whatever the sidecar says is present.
func (d *Downloader) runSegmented(ctx context.Context, job *store.DownloadJob, part string, st partState) error {
	link, err := d.Links.DownloadLink(ctx, job.FileID)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// WriteAt can land anywhere, so the file already has its final size; a
	// crash then leaves a part whose size matches even when ranges are missing.
	if err := f.Truncate(job.Size); err != nil {
		return err
	}

	s := &segmented{d: d, job: job, part: part, f: f, st: st, url: link.URL}
	for _, seg := range st.Segments {
		s.total.Add(seg.Done)
	}
	defer func() { job.Downloaded = s.total.Load() }()

	probe := -1
	for i := range st.Segments {
		if st.Segments[i].Done <= st.Segments[i].End-st.Segments[i].Start {
			probe = i
			break
		}
	}
	if probe < 0 {
		// Every byte is already on disk; the sidecar just has not been removed.
		if d.OnProgress != nil {
			d.OnProgress(s.total.Load())
		}
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return finalize(part, job)
	}

	// One byte of the next missing range decides whether the server honors
	// Range at all before eight connections are started.
	at := st.Segments[probe].Start + st.Segments[probe].Done
	first, err := s.openSegment(ctx, &segmentState{Start: at, End: at})
	if err != nil {
		return err
	}
	_ = first.Body.Close()
	switch first.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		if err := f.Truncate(0); err != nil {
			return err
		}
		removeState(part)
		return d.streamWhole(ctx, job, part, 0)
	default:
		return fmt.Errorf("transfer: range probe answered http %d", first.StatusCode)
	}

	s.save()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := s.report(sctx)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for i := range s.st.Segments {
		if s.st.Segments[i].Done > s.st.Segments[i].End-s.st.Segments[i].Start {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.segment(sctx, i); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	stop()

	if firstErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(firstErr, errRangeUnsupported) {
			if err := f.Truncate(0); err != nil {
				return err
			}
			removeState(part)
			return d.streamWhole(ctx, job, part, 0)
		}
		return firstErr
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if d.OnProgress != nil {
		d.OnProgress(s.total.Load())
	}
	if err := f.Close(); err != nil {
		return err
	}
	return finalize(part, job)
}

// segmented is one segmented download in progress: the file, the range map,
// the current signed URL (shared by every worker), and the running total.
type segmented struct {
	d     *Downloader
	job   *store.DownloadJob
	part  string
	f     *os.File
	total atomic.Int64

	mu  sync.Mutex
	st  partState
	url string
}

func (s *segmented) segmentState(i int) segmentState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Segments[i]
}

func (s *segmented) addDone(i int, n int64) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	s.st.Segments[i].Done += n
	s.mu.Unlock()
}

// openSegment requests a range, refreshing the signed URL when PikPak says it
// expired. A segment that loses the race keeps the URL the winner fetched.
func (s *segmented) openSegment(ctx context.Context, seg *segmentState) (*http.Response, error) {
	for attempt := 0; attempt < maxLinkFetches; attempt++ {
		s.mu.Lock()
		u := s.url
		s.mu.Unlock()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", seg.Start, seg.End))
		resp, err := s.d.client().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
			_ = resp.Body.Close()
			if err := s.refresh(ctx, u); err != nil {
				return nil, err
			}
			continue
		}
		return resp, nil
	}
	return nil, ErrLinkExpired
}

func (s *segmented) refresh(ctx context.Context, stale string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.url != stale {
		return nil
	}
	link, err := s.d.Links.DownloadLink(ctx, s.job.FileID)
	if err != nil {
		return err
	}
	if link.URL == "" {
		return ErrLinkExpired
	}
	s.url = link.URL
	return nil
}

// segment downloads one range, resuming it from its Done offset. A broken
// connection retries from the last written byte instead of failing the file.
func (s *segmented) segment(ctx context.Context, i int) error {
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		seg := s.segmentState(i)
		if seg.Done > seg.End-seg.Start {
			return nil
		}
		req := segmentState{Start: seg.Start + seg.Done, End: seg.End}
		resp, err := s.openSegment(ctx, &req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return errRangeUnsupported
		}
		if resp.StatusCode != http.StatusPartialContent {
			_ = resp.Body.Close()
			return fmt.Errorf("transfer: segment answered http %d", resp.StatusCode)
		}
		n, err := s.copySegment(ctx, i, resp.Body, req.Start)
		_ = resp.Body.Close()
		s.addDone(i, n)
		if err == nil {
			if n == 0 {
				failures++
			} else {
				failures = 0
			}
			if failures >= maxLinkFetches {
				return io.ErrUnexpectedEOF
			}
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		failures++
		if failures >= maxLinkFetches {
			return err
		}
	}
}

func (s *segmented) copySegment(ctx context.Context, i int, src io.Reader, at int64) (int64, error) {
	buf := make([]byte, segmentBuffer)
	var written int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := s.f.WriteAt(buf[:n], at+written); werr != nil {
				return written, werr
			}
			written += int64(n)
			s.total.Add(int64(n))
		}
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return written, ctx.Err()
			}
			return written, err
		}
	}
}

// report persists the range map and publishes the running total while the
// segments run.
func (s *segmented) report(ctx context.Context) (stop func()) {
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(reportInterval)
		defer t.Stop()
		var last int64
		for {
			select {
			case <-rctx.Done():
				return
			case <-t.C:
				n := s.total.Load()
				if n == last {
					continue
				}
				last = n
				s.save()
				if s.d.OnProgress != nil {
					s.d.OnProgress(n)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *segmented) save() {
	s.mu.Lock()
	raw, err := json.Marshal(s.st)
	s.mu.Unlock()
	if err != nil {
		return
	}
	tmp := s.part + stateSuffix + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return
	}
	_, werr := f.Write(raw)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, s.part+stateSuffix)
}

func stateExists(part string) bool {
	st, err := os.Lstat(part + stateSuffix)
	return err == nil && st.Mode()&os.ModeSymlink == 0
}

func removeState(part string) {
	_ = os.Remove(part + stateSuffix)
}

// loadPartState reads the sidecar back, rejecting anything that does not cover
// [0,size) exactly or does not match the part file on disk.
func loadPartState(part string, size int64) (partState, bool) {
	f, err := os.OpenFile(part+stateSuffix, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return partState{}, false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return partState{}, false
	}
	var st partState
	if err := json.Unmarshal(raw, &st); err != nil {
		return partState{}, false
	}
	if st.Size != size || len(st.Segments) == 0 {
		return partState{}, false
	}
	var next int64
	for _, seg := range st.Segments {
		if seg.Start != next || seg.End < seg.Start || seg.Done < 0 || seg.Done > seg.End-seg.Start+1 {
			return partState{}, false
		}
		next = seg.End + 1
	}
	if next != size {
		return partState{}, false
	}
	if fi, err := os.Stat(part); err != nil || fi.Size() != size {
		return partState{}, false
	}
	return st, true
}

// open fetches a link and starts the request, refreshing the link when PikPak
// answers 403/410 (the signed URL expired).
func (d *Downloader) open(ctx context.Context, fileID string, offset int64) (*http.Response, Link, error) {
	var lastErr error
	for attempt := 0; attempt < maxLinkFetches; attempt++ {
		link, err := d.Links.DownloadLink(ctx, fileID)
		if err != nil {
			return nil, Link{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
		if err != nil {
			return nil, Link{}, err
		}
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		resp, err := d.client().Do(req)
		if err != nil {
			return nil, Link{}, err
		}
		switch resp.StatusCode {
		case http.StatusOK, http.StatusPartialContent:
			return resp, link, nil
		case http.StatusForbidden, http.StatusGone:
			_ = resp.Body.Close()
			lastErr = ErrLinkExpired
			continue
		default:
			_ = resp.Body.Close()
			return nil, Link{}, fmt.Errorf("transfer: download answered http %d", resp.StatusCode)
		}
	}
	return nil, Link{}, lastErr
}

func (d *Downloader) copyBody(ctx context.Context, dst io.Writer, src io.Reader, offset int64) (int64, error) {
	buf := make([]byte, copyBufferSize)
	written := offset
	lastReport := time.Now()
	sinceReport := int64(0)

	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
			sinceReport += int64(n)
			if d.OnProgress != nil && (sinceReport >= reportBytes || time.Since(lastReport) >= reportInterval) {
				d.OnProgress(written)
				sinceReport = 0
				lastReport = time.Now()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return written, ctx.Err()
			}
			return written, err
		}
	}
	if d.OnProgress != nil {
		d.OnProgress(written)
	}
	return written, nil
}

func finalize(part string, job *store.DownloadJob) error {
	st, err := os.Stat(part)
	if err != nil {
		return err
	}
	if job.Size > 0 && st.Size() != job.Size {
		return ErrSizeMismatch
	}
	job.Downloaded = st.Size()
	if err := os.Rename(part, job.DestPath); err != nil {
		return err
	}
	// The sidecar describes a part file that no longer exists.
	removeState(part)
	return nil
}
