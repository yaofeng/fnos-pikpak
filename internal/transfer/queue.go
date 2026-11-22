package transfer

import (
	"context"
	"errors"
	"sync"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

// ClientProvider returns the PikPak client for a uid.
type ClientProvider func(ctx context.Context, uid int) (*pikpak.Client, error)

// ClientLinks adapts a PikPak client to LinkSource.
type ClientLinks struct{ C *pikpak.Client }

func (l ClientLinks) DownloadLink(ctx context.Context, fileID string) (Link, error) {
	got, err := l.C.GetDownloadLink(ctx, fileID)
	if err != nil {
		return Link{}, err
	}
	return Link{URL: got.URL, Size: got.Size}, nil
}

type runState struct {
	cancel context.CancelFunc
	uid    int
}

// Queue schedules download and upload jobs: per-user FIFO, global concurrency
// limit, and persisted progress. Pausing is a DB status plus a context cancel;
// the worker notices the cancel and leaves the job paused.
type Queue struct {
	Store   *store.Store
	Clients ClientProvider
	// NewDownloader/NewUploader default to real engines built from Clients;
	// tests inject fakes.
	NewDownloader func(uid int) (*Downloader, error)
	NewUploader   func(uid int) (*Uploader, error)
	MaxGlobal     int
	Tick          time.Duration

	mu      sync.Mutex
	running map[string]*runState
	perUser map[int]int
	wake    chan struct{}
	wg      sync.WaitGroup
}

func (q *Queue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.running == nil {
		q.running = map[string]*runState{}
		q.perUser = map[int]int{}
		q.wake = make(chan struct{}, 1)
	}
	if q.MaxGlobal <= 0 {
		q.MaxGlobal = 2
	}
	if q.Tick <= 0 {
		q.Tick = time.Second
	}
	q.mu.Unlock()
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		q.loop(ctx)
	}()
}

// Wait blocks until the scheduler has stopped (used on shutdown).
func (q *Queue) Wait() { q.wg.Wait() }

func (q *Queue) Notify() {
	q.mu.Lock()
	wake := q.wake
	q.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// CancelRunning stops a job's worker. The caller has already written the new
// status, so the worker will not overwrite it as a failure.
func (q *Queue) CancelRunning(jobID string) {
	q.mu.Lock()
	st := q.running[jobID]
	q.mu.Unlock()
	if st != nil {
		st.cancel()
	}
}

func (q *Queue) loop(ctx context.Context) {
	ticker := time.NewTicker(q.Tick)
	defer ticker.Stop()
	for {
		q.schedule(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-q.wake:
		}
	}
}

func (q *Queue) schedule(ctx context.Context) {
	q.mu.Lock()
	capacity := q.MaxGlobal - len(q.running)
	q.mu.Unlock()
	if capacity <= 0 {
		return
	}

	downloads, err := q.Store.QueuedDownloadJobs(ctx, 50)
	if err == nil {
		for _, job := range downloads {
			if !q.reserve(job.ID, job.UID) {
				continue
			}
			capacity--
			q.wg.Add(1)
			go func(j store.DownloadJob) {
				defer q.wg.Done()
				defer q.release(j.ID)
				q.runDownload(ctx, j)
			}(job)
			if capacity <= 0 {
				return
			}
		}
	}

	uploads, err := q.Store.QueuedUploadJobs(ctx, 50)
	if err == nil {
		for _, job := range uploads {
			if !q.reserve(job.ID, job.UID) {
				continue
			}
			capacity--
			q.wg.Add(1)
			go func(j store.UploadJob) {
				defer q.wg.Done()
				defer q.release(j.ID)
				q.runUpload(ctx, j)
			}(job)
			if capacity <= 0 {
				return
			}
		}
	}
}

// reserve claims a slot for a job. It returns false when the job was already
// running (a duplicate schedule) or the user already has a transfer.
func (q *Queue) reserve(id string, uid int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.running[id]; ok {
		return false
	}
	if q.perUser[uid] > 0 {
		return false
	}
	q.running[id] = &runState{uid: uid}
	q.perUser[uid]++
	return true
}

func (q *Queue) release(id string) {
	q.mu.Lock()
	if st, ok := q.running[id]; ok {
		q.perUser[st.uid]--
		delete(q.running, id)
	}
	q.mu.Unlock()
	q.Notify()
}

func (q *Queue) setCancel(id string, cancel context.CancelFunc) {
	q.mu.Lock()
	if st, ok := q.running[id]; ok {
		st.cancel = cancel
	}
	q.mu.Unlock()
}

func (q *Queue) runDownload(parent context.Context, job store.DownloadJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	q.setCancel(job.ID, cancel)

	job.Status = "running"
	job.ErrorCode = ""
	job.ErrorMessage = ""
	_ = q.Store.PutDownloadJob(ctx, job)

	d, err := q.downloader(job.UID, &job)
	if err != nil {
		q.failDownload(job, "client_unavailable", err)
		return
	}
	err = d.Run(ctx, &job)

	cur, getErr := q.Store.GetDownloadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if getErr != nil {
		cur = job
	}
	switch {
	case err == nil:
		cur.Status = "done"
		cur.Downloaded = cur.Size
		cur.ErrorCode = ""
		cur.ErrorMessage = ""
	case errors.Is(err, context.Canceled):
		// Pause/cancel already wrote the status; leave it unless nothing did.
		if cur.Status == "running" {
			cur.Status = "canceled"
		}
	case errors.Is(err, ErrInsufficientSpace):
		cur.Status = "failed"
		cur.ErrorCode = "insufficient_space"
		cur.ErrorMessage = "磁盘剩余空间不足"
	case errors.Is(err, ErrSizeMismatch):
		cur.Status = "failed"
		cur.ErrorCode = "size_mismatch"
		cur.ErrorMessage = "下载大小与文件不一致"
	default:
		cur.Status = "failed"
		cur.ErrorCode = "download_failed"
		cur.ErrorMessage = probe.Redact(err.Error())
	}
	_ = q.Store.PutDownloadJob(context.WithoutCancel(parent), cur)
}

func (q *Queue) downloader(uid int, job *store.DownloadJob) (*Downloader, error) {
	if q.NewDownloader != nil {
		return q.NewDownloader(uid)
	}
	client, err := q.Clients(context.Background(), uid)
	if err != nil {
		return nil, err
	}
	d := &Downloader{Links: ClientLinks{C: client}}
	d.OnProgress = func(n int64) {
		job.Downloaded = n
		_ = q.Store.PutDownloadJob(context.Background(), *job)
	}
	return d, nil
}

func (q *Queue) failDownload(job store.DownloadJob, code string, err error) {
	job.Status = "failed"
	job.ErrorCode = code
	job.ErrorMessage = probe.Redact(err.Error())
	_ = q.Store.PutDownloadJob(context.Background(), job)
}

func (q *Queue) runUpload(parent context.Context, job store.UploadJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	q.setCancel(job.ID, cancel)

	job.Status = "running"
	job.ErrorCode = ""
	job.ErrorMessage = ""
	_ = q.Store.PutUploadJob(ctx, job)

	u, err := q.uploader(job.UID)
	if err != nil {
		q.failUpload(job, "client_unavailable", err)
		return
	}
	policy := q.policy(job.UID)
	err = u.Run(ctx, &job, policy)

	cur, getErr := q.Store.GetUploadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if getErr != nil {
		cur = job
	}
	switch {
	case err == nil:
		cur.Status = "done"
		cur.Uploaded = cur.Size
		cur.ErrorCode = ""
		cur.ErrorMessage = ""
	case errors.Is(err, ErrSkipped):
		cur.Status = "skipped"
		cur.ErrorCode = "conflict"
		cur.ErrorMessage = "目标目录已有同名文件，已跳过"
	case errors.Is(err, context.Canceled):
		if cur.Status == "running" {
			cur.Status = "canceled"
		}
	default:
		cur.Status = "failed"
		cur.ErrorCode = "upload_failed"
		cur.ErrorMessage = probe.Redact(err.Error())
	}
	_ = q.Store.PutUploadJob(context.WithoutCancel(parent), cur)
}

func (q *Queue) uploader(uid int) (*Uploader, error) {
	if q.NewUploader != nil {
		return q.NewUploader(uid)
	}
	client, err := q.Clients(context.Background(), uid)
	if err != nil {
		return nil, err
	}
	return &Uploader{Client: client}, nil
}

func (q *Queue) failUpload(job store.UploadJob, code string, err error) {
	job.Status = "failed"
	job.ErrorCode = code
	job.ErrorMessage = probe.Redact(err.Error())
	_ = q.Store.PutUploadJob(context.Background(), job)
}

func (q *Queue) policy(uid int) ConflictPolicy {
	value := ""
	if v, err := q.Store.GetUserSetting(context.Background(), uid, "conflict"); err == nil {
		value = v
	} else if v, err := q.Store.GetSetting(context.Background(), "conflict"); err == nil {
		value = v
	}
	switch ConflictPolicy(value) {
	case PolicySkip, PolicyOverwrite:
		return ConflictPolicy(value)
	default:
		return PolicyRename
	}
}
