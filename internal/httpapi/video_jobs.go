package httpapi

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// videoJobTTL bounds how long a finished prepare result stays readable. The
// stream URL inside it has its own, longer-lived token.
const videoJobTTL = 30 * time.Minute

const (
	videoJobRunning = "running"
	videoJobReady   = "ready"
	videoJobFailed  = "failed"
)

// videoPrepareResult is what a finished prepare hands the browser: the copy in
// Pack From Shared (and the account whose drive holds it) plus a signed URL
// that plays it.
type videoPrepareResult struct {
	FileID string `json:"file_id"`
	// AccountID names the account the copy lives on and the stream token
	// spends; empty only for results minted before accounts existed.
	AccountID string `json:"account_id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	StreamURL string `json:"stream_url"`
	// DirectURL is the original CDN link, set only when the user chose IINA
	// in 设置: an external player cannot reach the server's proxy, and the
	// choice is theirs to make.
	DirectURL string `json:"direct_url,omitempty"`
}

type videoJob struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Stage string `json:"stage"`
	Error string `json:"error,omitempty"`

	videoPrepareResult

	uid     int
	expires time.Time
}

// videoJobs runs one prepare per user at a time. Preparing may restore,
// delete and move files, so two overlapping runs would race over the same
// folders and the same quota.
type videoJobs struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]videoJob
	busy  map[int]string
}

func newVideoJobs() *videoJobs {
	return &videoJobs{
		ttl:   videoJobTTL,
		items: map[string]videoJob{},
		busy:  map[int]string{},
	}
}

// Start begins run in the background and returns its job id. A second start
// for the same user is refused with ok=false until the first settles.
func (j *videoJobs) Start(uid int, run func(context.Context) (videoPrepareResult, error)) (string, bool) {
	now := time.Now()
	j.mu.Lock()
	j.purgeLocked(now)
	if _, taken := j.busy[uid]; taken {
		j.mu.Unlock()
		return "", false
	}
	id := newVideoJobID()
	j.items[id] = videoJob{
		ID: id, State: videoJobRunning, Stage: "准备中",
		uid: uid, expires: now.Add(j.ttl),
	}
	j.busy[uid] = id
	j.mu.Unlock()

	// The job outlives the request that started it: polling reads the result
	// long after that request is gone.
	go func() {
		result, err := run(context.Background())
		j.mu.Lock()
		job := j.items[id]
		if err != nil {
			job.State = videoJobFailed
			job.Error = err.Error()
			job.Stage = ""
		} else {
			job.State = videoJobReady
			job.Stage = ""
			job.videoPrepareResult = result
		}
		job.expires = time.Now().Add(j.ttl)
		j.items[id] = job
		delete(j.busy, uid)
		j.mu.Unlock()
	}()
	return id, true
}

// Get returns one of the user's jobs. Another user's job reads as missing.
func (j *videoJobs) Get(uid int, id string, now time.Time) (videoJob, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.items[id]
	if !ok || job.uid != uid || now.After(job.expires) {
		return videoJob{}, false
	}
	return job, true
}

func (j *videoJobs) purgeLocked(now time.Time) {
	for id, job := range j.items {
		if now.After(job.expires) {
			delete(j.items, id)
		}
	}
}

func newVideoJobID() string {
	b := make([]byte, 8)
	if _, err := crand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
