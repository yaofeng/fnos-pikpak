package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func sampleJob(id string, uid int, status string) DownloadJob {
	return DownloadJob{
		ID: id, UID: uid, FileID: "f-" + id, FileName: id + ".mkv",
		DestDir: "/vol1/1001/pikpak", DestPath: "/vol1/1001/pikpak/" + id + ".mkv",
		Size: 100, Downloaded: 40, Status: status,
	}
}

func TestDownloadJobRoundTrip(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	job := sampleJob("j1", 1000, "queued")
	if err := s.PutDownloadJob(ctx, job); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.GetDownloadJob(ctx, 1000, "j1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FileName != "j1.mkv" || got.Size != 100 || got.Downloaded != 40 || got.Status != "queued" {
		t.Fatalf("got %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps missing: %+v", got)
	}

	job.Status = "running"
	job.Downloaded = 80
	if err := s.PutDownloadJob(ctx, job); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetDownloadJob(ctx, 1000, "j1")
	if got.Status != "running" || got.Downloaded != 80 {
		t.Fatalf("after update: %+v", got)
	}
}

func TestDownloadJobIsScopedToUser(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	_ = s.PutDownloadJob(ctx, sampleJob("j1", 1000, "queued"))
	_ = s.PutDownloadJob(ctx, sampleJob("j2", 1001, "queued"))

	if _, err := s.GetDownloadJob(ctx, 1001, "j1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user err = %v, want ErrNotFound", err)
	}
	jobs, err := s.ListDownloadJobs(ctx, 1000)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "j1" {
		t.Fatalf("jobs = %+v err = %v", jobs, err)
	}
	if err := s.DeleteDownloadJob(ctx, 1001, "j1"); err != nil {
		t.Fatalf("delete other: %v", err)
	}
	if _, err := s.GetDownloadJob(ctx, 1000, "j1"); err != nil {
		t.Fatalf("another user's delete removed the job: %v", err)
	}
}

func TestListDownloadJobsFiltersStatus(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	for _, st := range []string{"queued", "running", "done", "failed"} {
		_ = s.PutDownloadJob(ctx, sampleJob("j-"+st, 1000, st))
	}
	jobs, err := s.ListDownloadJobs(ctx, 1000, "queued", "running")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestResetRunningDownloadJobs(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	_ = s.PutDownloadJob(ctx, sampleJob("j1", 1000, "running"))
	_ = s.PutDownloadJob(ctx, sampleJob("j2", 1000, "queued"))
	_ = s.PutDownloadJob(ctx, sampleJob("j3", 1000, "done"))

	if err := s.ResetRunningDownloadJobs(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if j, _ := s.GetDownloadJob(ctx, 1000, "j1"); j.Status != "queued" {
		t.Fatalf("j1 = %q", j.Status)
	}
	if j, _ := s.GetDownloadJob(ctx, 1000, "j3"); j.Status != "done" {
		t.Fatalf("j3 = %q", j.Status)
	}
}

func TestDownloadJobTimestampsSurviveUpdate(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	job := sampleJob("j1", 1000, "queued")
	_ = s.PutDownloadJob(ctx, job)
	first, _ := s.GetDownloadJob(ctx, 1000, "j1")

	time.Sleep(10 * time.Millisecond)
	job.Downloaded = 99
	_ = s.PutDownloadJob(ctx, job)
	second, _ := s.GetDownloadJob(ctx, 1000, "j1")

	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at changed: %v → %v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("updated_at did not advance: %v → %v", first.UpdatedAt, second.UpdatedAt)
	}
}
