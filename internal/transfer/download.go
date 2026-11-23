package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
)

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
	if job.Size > 0 && offset == job.Size {
		return finalize(part, job)
	}

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
	return os.Rename(part, job.DestPath)
}
