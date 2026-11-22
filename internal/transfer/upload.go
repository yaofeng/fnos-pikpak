package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

// ErrSkipped means the conflict policy chose not to upload this file.
var ErrSkipped = errors.New("transfer: upload skipped because the remote file exists")

type ConflictPolicy string

const (
	PolicyRename    ConflictPolicy = "rename"
	PolicySkip      ConflictPolicy = "skip"
	PolicyOverwrite ConflictPolicy = "overwrite"
)

// UploadClient is the slice of the PikPak client the uploader needs.
type UploadClient interface {
	ListFiles(ctx context.Context, parentID string, o pikpak.ListOptions) (pikpak.FilePage, error)
	RequestUpload(ctx context.Context, in pikpak.UploadRequest) (pikpak.UploadTicket, error)
	CompleteUpload(ctx context.Context, ticket pikpak.UploadTicket, src io.Reader) error
	WaitFileComplete(ctx context.Context, fileID string, timeout time.Duration) (pikpak.File, error)
	BatchTrash(ctx context.Context, ids []string) (string, error)
	BatchUntrash(ctx context.Context, ids []string) (string, error)
}

type Uploader struct {
	Client UploadClient
	// WaitTimeout bounds the wait for PikPak to finish processing an upload.
	WaitTimeout time.Duration
	// CheckDir re-validates the source directory at read time: grants can be
	// revoked while a job waits in the queue.
	CheckDir func(dir string) error
}

func (u *Uploader) Run(ctx context.Context, job *store.UploadJob, policy ConflictPolicy) (err error) {
	if u.CheckDir != nil {
		if err := u.CheckDir(filepath.Dir(job.LocalPath)); err != nil {
			return err
		}
	}
	// The API resolved this path when the job was queued; O_NOFOLLOW refuses
	// a link swapped in afterwards, which would read a file the user was
	// never granted.
	f, err := os.OpenFile(job.LocalPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return ErrUnsafePath
		}
		return err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("transfer: %s is not a regular file", job.LocalPath)
	}
	job.Size = st.Size()

	existing, names, err := u.scan(ctx, job)
	if err != nil {
		return err
	}
	switch {
	case existing != nil && policy == PolicySkip:
		return ErrSkipped
	case existing != nil && policy == PolicyRename:
		job.RemoteName = uniqueName(job.RemoteName, names)
	case existing != nil && policy == PolicyOverwrite:
		if _, err := u.Client.BatchTrash(ctx, []string{existing.ID}); err != nil {
			return err
		}
		// If anything after the trash fails, put the old file back so the
		// folder does not end up with neither version.
		defer func() {
			if err != nil {
				_, _ = u.Client.BatchUntrash(context.Background(), []string{existing.ID})
			}
		}()
	}

	gcid, err := pikpak.GCID(f, job.Size)
	if err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}

	ticket, err := u.Client.RequestUpload(ctx, pikpak.UploadRequest{
		Name:     job.RemoteName,
		Size:     job.Size,
		GCID:     gcid,
		ParentID: job.ParentID,
	})
	if err != nil {
		return err
	}

	instant := ticket.Resumable == nil
	if !instant {
		if err := u.Client.CompleteUpload(ctx, ticket, f); err != nil {
			return err
		}
	}

	file := ticket.File
	if file == nil || file.ID == "" {
		file, err = u.lookup(ctx, job)
		if err != nil {
			return err
		}
	}
	if file != nil && file.ID != "" && file.Phase != "PHASE_TYPE_COMPLETE" {
		timeout := u.WaitTimeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		if _, err := u.Client.WaitFileComplete(ctx, file.ID, timeout); err != nil {
			return err
		}
	}

	job.Uploaded = job.Size
	return nil
}

// scan lists the target folder once and returns the conflicting file plus
// every name in it, which the rename policy needs.
func (u *Uploader) scan(ctx context.Context, job *store.UploadJob) (*pikpak.File, map[string]bool, error) {
	page, err := u.Client.ListFiles(ctx, job.ParentID, pikpak.ListOptions{Limit: 200})
	if err != nil {
		return nil, nil, err
	}
	names := make(map[string]bool, len(page.Items)+1)
	names[job.RemoteName] = true
	var existing *pikpak.File
	for i := range page.Items {
		f := &page.Items[i]
		names[f.Name] = true
		if f.Name == job.RemoteName && f.Kind != "drive#folder" {
			existing = f
		}
	}
	return existing, names, nil
}

func (u *Uploader) lookup(ctx context.Context, job *store.UploadJob) (*pikpak.File, error) {
	page, err := u.Client.ListFiles(ctx, job.ParentID, pikpak.ListOptions{Limit: 200})
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].Name == job.RemoteName {
			return &page.Items[i], nil
		}
	}
	return nil, nil
}

// existingNames was folded into scan.

// uniqueName appends " (n)" before the extension until the name is free.
func uniqueName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if !taken[candidate] {
			return candidate
		}
	}
	return fmt.Sprintf("%s (%d)%s", base, time.Now().UnixNano(), ext)
}
