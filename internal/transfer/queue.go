package transfer

import (
	"context"
	"errors"
	"sort"
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
	// CheckDir re-validates a destination directory at write time; the API
	// checked it when the job was created.
	CheckDir func(ctx context.Context, uid int, dir string) error
	// MaxGlobal caps transfers across all users; PerUser caps one user's
	// concurrent transfers (default 1).
	MaxGlobal int
	PerUser   int
	Tick      time.Duration

	mu      sync.Mutex
	running map[string]*runState
	perUser map[int]int
	wake    chan struct{}
	wg      sync.WaitGroup
	// lastServed orders users for the next scheduling round: the user served
	// longest ago goes first. Only the scheduler goroutine touches it.
	lastServed map[int]uint64
	serveSeq   uint64
}

func (q *Queue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.running == nil {
		q.running = map[string]*runState{}
		q.perUser = map[int]int{}
		q.wake = make(chan struct{}, 1)
		q.lastServed = map[int]uint64{}
		// 0 means "never served", so the first turn starts at 1.
		q.serveSeq = 1
	}
	if q.MaxGlobal <= 0 {
		q.MaxGlobal = 2
	}
	if q.PerUser <= 0 {
		q.PerUser = 1
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

// CancelUser stops every running job of uid. Unbinding the account is the
// caller: the credentials are gone, so those workers can never finish.
func (q *Queue) CancelUser(uid int) {
	q.mu.Lock()
	var cancels []context.CancelFunc
	for _, st := range q.running {
		if st.uid == uid && st.cancel != nil {
			cancels = append(cancels, st.cancel)
		}
	}
	q.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
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

// pendingJob is one queued transfer of either kind, in creation order.
type pendingJob struct {
	uid      int
	id       string
	created  time.Time
	download *store.DownloadJob
	upload   *store.UploadJob
}

// schedule fills the free slots fairly: jobs are grouped per user, and users
// take turns one job at a time, so a user with a long queue cannot hold every
// slot. Downloads and uploads share the same turn order, oldest first.
func (q *Queue) schedule(ctx context.Context) {
	q.mu.Lock()
	capacity := q.MaxGlobal - len(q.running)
	q.mu.Unlock()
	if capacity <= 0 {
		return
	}

	downloads, _ := q.Store.QueuedDownloadJobs(ctx, 50)
	uploads, _ := q.Store.QueuedUploadJobs(ctx, 50)
	merged := make([]pendingJob, 0, len(downloads)+len(uploads))
	i, j := 0, 0
	for i < len(downloads) || j < len(uploads) {
		takeDownload := i < len(downloads) &&
			(j >= len(uploads) || !downloads[i].CreatedAt.After(uploads[j].CreatedAt))
		if takeDownload {
			d := downloads[i]
			i++
			merged = append(merged, pendingJob{uid: d.UID, id: d.ID, created: d.CreatedAt, download: &d})
			continue
		}
		u := uploads[j]
		j++
		merged = append(merged, pendingJob{uid: u.UID, id: u.ID, created: u.CreatedAt, upload: &u})
	}

	// One FIFO per user. Users take turns: the one served longest ago starts
	// the next round, so a long queue at one user does not starve the others.
	order := make([]int, 0, len(merged))
	queues := make(map[int][]pendingJob, len(merged))
	for _, p := range merged {
		if _, ok := queues[p.uid]; !ok {
			order = append(order, p.uid)
		}
		queues[p.uid] = append(queues[p.uid], p)
	}
	sort.SliceStable(order, func(a, b int) bool {
		return q.lastServed[order[a]] < q.lastServed[order[b]]
	})

	for capacity > 0 {
		progress := false
		for _, uid := range order {
			if capacity <= 0 {
				return
			}
			list := queues[uid]
			for len(list) > 0 {
				p := list[0]
				list = list[1:]
				if !q.reserve(p.id, p.uid) {
					continue
				}
				queues[uid] = list
				q.lastServed[uid] = q.serveSeq
				q.serveSeq++
				capacity--
				progress = true
				if p.download != nil {
					job := *p.download
					q.wg.Add(1)
					go func() {
						defer q.wg.Done()
						defer q.release(job.ID)
						q.runDownload(ctx, job)
					}()
				} else {
					job := *p.upload
					q.wg.Add(1)
					go func() {
						defer q.wg.Done()
						defer q.release(job.ID)
						q.runUpload(ctx, job)
					}()
				}
				break // one job per user per turn
			}
			queues[uid] = list
		}
		if !progress {
			return
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
	if q.perUser[uid] >= q.PerUser {
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

	// Claim atomically: a pause or delete that landed first wins, and this
	// worker leaves the row alone instead of running a job nobody wants.
	claimed, err := q.Store.StartDownloadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if err != nil || !claimed {
		return
	}
	job.Status = "running"
	job.ErrorCode = ""
	job.ErrorMessage = ""

	d, err := q.downloader(job.UID, &job)
	if err != nil {
		q.failDownload(job, "client_unavailable", err)
		return
	}
	err = d.Run(ctx, &job)

	cur, getErr := q.Store.GetDownloadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if getErr != nil {
		// Deleted while it ran; there is no row left to update.
		return
	}
	switch {
	case err == nil:
		cur.Status = "done"
		cur.Downloaded = cur.Size
		cur.ErrorCode = ""
		cur.ErrorMessage = ""
	case errors.Is(err, context.Canceled):
		switch {
		case cur.Status != "running":
			// Pause/cancel already wrote the status.
		case parent.Err() != nil:
			// The service is stopping. Put the job back in the queue so the
			// next start resumes it instead of showing a canceled transfer.
			cur.Status = "queued"
		default:
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
	_ = q.Store.FinishDownloadJob(context.WithoutCancel(parent), cur.UID, cur.ID,
		cur.Status, cur.ErrorCode, cur.ErrorMessage, cur.Downloaded)
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
	if q.CheckDir != nil {
		uid := job.UID
		d.CheckDir = func(dir string) error { return q.CheckDir(context.Background(), uid, dir) }
	}
	d.OnProgress = func(n int64) {
		job.Downloaded = n
		_ = q.Store.SetDownloadProgress(context.Background(), job.UID, job.ID, n)
	}
	return d, nil
}

func (q *Queue) failDownload(job store.DownloadJob, code string, err error) {
	_ = q.Store.FinishDownloadJob(context.Background(), job.UID, job.ID,
		"failed", code, probe.Redact(err.Error()), job.Downloaded)
}

func (q *Queue) runUpload(parent context.Context, job store.UploadJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	q.setCancel(job.ID, cancel)

	claimed, err := q.Store.StartUploadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if err != nil || !claimed {
		return
	}
	job.Status = "running"
	job.ErrorCode = ""
	job.ErrorMessage = ""

	u, err := q.uploader(job)
	if err != nil {
		q.failUpload(job, "client_unavailable", err)
		return
	}
	policy := q.policy(job.UID)
	err = u.Run(ctx, &job, policy)

	cur, getErr := q.Store.GetUploadJob(context.WithoutCancel(parent), job.UID, job.ID)
	if getErr != nil {
		return
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
		switch {
		case cur.Status != "running":
		case parent.Err() != nil:
			cur.Status = "queued"
		default:
			cur.Status = "canceled"
		}
	default:
		cur.Status = "failed"
		cur.ErrorCode = "upload_failed"
		cur.ErrorMessage = probe.Redact(err.Error())
	}
	_ = q.Store.FinishUploadJob(context.WithoutCancel(parent), cur.UID, cur.ID,
		cur.Status, cur.ErrorCode, cur.ErrorMessage, cur.Uploaded)
}

func (q *Queue) uploader(job store.UploadJob) (*Uploader, error) {
	if q.NewUploader != nil {
		return q.NewUploader(job.UID)
	}
	client, err := q.Clients(context.Background(), job.UID)
	if err != nil {
		return nil, err
	}
	u := &Uploader{Client: client}
	if q.CheckDir != nil {
		uid := job.UID
		u.CheckDir = func(dir string) error { return q.CheckDir(context.Background(), uid, dir) }
	}
	u.OnProgress = func(n int64) {
		_ = q.Store.SetUploadProgress(context.Background(), job.UID, job.ID, n)
	}
	return u, nil
}

func (q *Queue) failUpload(job store.UploadJob, code string, err error) {
	_ = q.Store.FinishUploadJob(context.Background(), job.UID, job.ID,
		"failed", code, probe.Redact(err.Error()), job.Uploaded)
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
