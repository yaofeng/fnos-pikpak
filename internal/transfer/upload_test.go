package transfer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

type fakeUploadClient struct {
	files     []pikpak.File
	ticket    pikpak.UploadTicket
	requested []pikpak.UploadRequest
	completed int
	waited    []string
	trashed   [][]string
	untrashed [][]string
	fail      error
}

func (f *fakeUploadClient) ListFiles(context.Context, string, pikpak.ListOptions) (pikpak.FilePage, error) {
	return pikpak.FilePage{Items: f.files}, nil
}

func (f *fakeUploadClient) RequestUpload(_ context.Context, in pikpak.UploadRequest) (pikpak.UploadTicket, error) {
	f.requested = append(f.requested, in)
	if f.fail != nil && f.completed == 0 {
		return pikpak.UploadTicket{}, f.fail
	}
	t := f.ticket
	t.Size = in.Size
	if t.File == nil {
		t.File = &pikpak.File{ID: "new", Name: in.Name, Size: in.Size}
	}
	return t, nil
}

func (f *fakeUploadClient) CompleteUpload(context.Context, pikpak.UploadTicket, io.Reader) error {
	f.completed++
	return f.fail
}

func (f *fakeUploadClient) WaitFileComplete(_ context.Context, id string, _ time.Duration) (pikpak.File, error) {
	f.waited = append(f.waited, id)
	return pikpak.File{ID: id, Phase: "PHASE_TYPE_COMPLETE"}, nil
}

func (f *fakeUploadClient) BatchTrash(_ context.Context, ids []string) (string, error) {
	f.trashed = append(f.trashed, ids)
	return "", nil
}

func (f *fakeUploadClient) BatchUntrash(_ context.Context, ids []string) (string, error) {
	f.untrashed = append(f.untrashed, ids)
	return "", nil
}

func uploadJob(t *testing.T, name string) store.UploadJob {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("hello pikpak"), 0o644); err != nil {
		t.Fatal(err)
	}
	return store.UploadJob{
		ID: "u1", UID: 1000, LocalPath: path, ParentID: "p1",
		RemoteName: name, Size: 12, Status: "running",
	}
}

func TestUploadComputesGCIDAndCompletes(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyRename); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fake.requested) != 1 {
		t.Fatalf("requested = %+v", fake.requested)
	}
	want, err := pikpak.GCIDFile(job.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	if fake.requested[0].GCID != want {
		t.Fatalf("gcid = %q, want %q", fake.requested[0].GCID, want)
	}
	if fake.requested[0].Size != 12 || fake.requested[0].ParentID != "p1" {
		t.Fatalf("request = %+v", fake.requested[0])
	}
}

func TestUploadRenamePolicyPicksAFreeName(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{files: []pikpak.File{
		{ID: "x1", Name: "a.mkv", Kind: "drive#file"},
		{ID: "x2", Name: "a (1).mkv", Kind: "drive#file"},
	}}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyRename); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if job.RemoteName != "a (2).mkv" {
		t.Fatalf("remote name = %q", job.RemoteName)
	}
	if fake.requested[0].Name != "a (2).mkv" {
		t.Fatalf("requested name = %q", fake.requested[0].Name)
	}
}

func TestUploadSkipPolicyLeavesExistingFile(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{files: []pikpak.File{{ID: "x1", Name: "a.mkv", Kind: "drive#file"}}}
	u := &Uploader{Client: fake}
	err := u.Run(context.Background(), &job, PolicySkip)
	if !errors.Is(err, ErrSkipped) {
		t.Fatalf("err = %v, want ErrSkipped", err)
	}
	if len(fake.requested) != 0 {
		t.Fatalf("skipped upload still requested: %+v", fake.requested)
	}
	if len(fake.trashed) != 0 {
		t.Fatalf("skip trashed the existing file: %v", fake.trashed)
	}
}

func TestUploadOverwriteTrashesThenUploads(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{files: []pikpak.File{{ID: "x1", Name: "a.mkv", Kind: "drive#file"}}}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyOverwrite); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fake.trashed) != 1 || fake.trashed[0][0] != "x1" {
		t.Fatalf("trashed = %v", fake.trashed)
	}
	if job.RemoteName != "a.mkv" {
		t.Fatalf("overwrite renamed the file: %q", job.RemoteName)
	}
	if len(fake.untrashed) != 0 {
		t.Fatalf("successful overwrite rolled back: %v", fake.untrashed)
	}
}

func TestUploadOverwriteRollsBackOnFailure(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{
		files: []pikpak.File{{ID: "x1", Name: "a.mkv", Kind: "drive#file"}},
		fail:  errors.New("oss broke"),
	}
	u := &Uploader{Client: fake}
	err := u.Run(context.Background(), &job, PolicyOverwrite)
	if err == nil {
		t.Fatal("upload failure was swallowed")
	}
	if len(fake.untrashed) != 1 || fake.untrashed[0][0] != "x1" {
		t.Fatalf("existing file was not restored: %v", fake.untrashed)
	}
}

func TestUploadInstantSkipsTransfer(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{ticket: pikpak.UploadTicket{
		File: &pikpak.File{ID: "f9", Name: "a.mkv", Phase: "PHASE_TYPE_COMPLETE"},
	}}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyRename); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.completed != 0 {
		t.Fatalf("instant upload sent file bytes (%d completions)", fake.completed)
	}
	if len(fake.waited) != 0 {
		t.Fatalf("instant upload waited for a phase: %v", fake.waited)
	}
}

func TestUploadResumableWaitsForComplete(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	fake := &fakeUploadClient{ticket: pikpak.UploadTicket{
		File:      &pikpak.File{ID: "f9", Name: "a.mkv", Phase: "PHASE_TYPE_PENDING"},
		Resumable: &pikpak.Resumable{Provider: "PROVIDER_ALIYUN"},
	}}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyRename); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.completed != 1 {
		t.Fatalf("completions = %d", fake.completed)
	}
	if len(fake.waited) != 1 || fake.waited[0] != "f9" {
		t.Fatalf("waited = %v", fake.waited)
	}
}

func TestUploadMissingLocalFileFails(t *testing.T) {
	job := uploadJob(t, "a.mkv")
	job.LocalPath = filepath.Join(t.TempDir(), "gone.mkv")
	fake := &fakeUploadClient{}
	u := &Uploader{Client: fake}
	if err := u.Run(context.Background(), &job, PolicyRename); err == nil {
		t.Fatal("missing source file was accepted")
	}
	if len(fake.requested) != 0 {
		t.Fatal("missing source still hit the API")
	}
}
