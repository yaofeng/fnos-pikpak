package pikpak

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"fnos-pikpak/internal/probe"
)

// ErrTaskTimeout means an offline task did not reach a terminal phase before
// the caller's deadline.
var ErrTaskTimeout = errors.New("pikpak: task did not finish in time")

// Task is one cloud-download task. FileSize arrives as a string.
type Task struct {
	ID       string
	Type     string
	Name     string
	FileID   string
	FileName string
	FileSize int64
	Phase    string
	Progress int
	Message  string
}

func (t *Task) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Name     string          `json:"name"`
		FileID   string          `json:"file_id"`
		FileName string          `json:"file_name"`
		FileSize json.RawMessage `json:"file_size"`
		Phase    string          `json:"phase"`
		Progress int             `json:"progress"`
		Message  string          `json:"message"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*t = Task{
		ID:       raw.ID,
		Type:     raw.Type,
		Name:     raw.Name,
		FileID:   raw.FileID,
		FileName: raw.FileName,
		FileSize: parseSize(raw.FileSize),
		Phase:    raw.Phase,
		Progress: raw.Progress,
		Message:  raw.Message,
	}
	return nil
}

// CreateOffline asks PikPak to download link into parentID ("" is the root).
func (c *Client) CreateOffline(ctx context.Context, link, parentID string) (Task, error) {
	body := map[string]any{
		"kind":        "drive#file",
		"upload_type": "UPLOAD_TYPE_URL",
		"url":         map[string]string{"url": link},
		"folder_type": "DOWNLOAD",
	}
	if parentID != "" {
		body["parent_id"] = parentID
	}

	var raw struct {
		Task Task `json:"task"`
	}
	if err := c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files", body, &raw); err != nil {
		return Task{}, err
	}
	return raw.Task, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (Task, error) {
	var task Task
	err := c.do(ctx, http.MethodGet, c.BaseDrive+"/drive/v1/tasks/"+url.PathEscape(id), nil, &task)
	return task, err
}

// DeleteTask removes the task; deleteFiles also drops the downloaded file so a
// probe leaves nothing behind.
func (c *Client) DeleteTask(ctx context.Context, id string, deleteFiles bool) error {
	q := url.Values{}
	q.Set("task_ids", id)
	q.Set("delete_files", strconv.FormatBool(deleteFiles))
	return c.do(ctx, http.MethodDelete, c.BaseDrive+"/drive/v1/tasks?"+q.Encode(), nil, nil)
}

// WaitTask polls until the task reaches a terminal phase, the timeout expires,
// or a request fails. It returns the last task it saw.
func (c *Client) WaitTask(ctx context.Context, id string, timeout, interval time.Duration) (Task, error) {
	deadline := time.Now().Add(timeout)
	if interval <= 0 {
		interval = time.Second
	}
	for {
		task, err := c.GetTask(ctx, id)
		if err != nil {
			return task, err
		}
		switch task.Phase {
		case "PHASE_TYPE_COMPLETE", "PHASE_TYPE_ERROR":
			return task, nil
		}
		if time.Now().After(deadline) {
			return task, ErrTaskTimeout
		}
		select {
		case <-ctx.Done():
			return task, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// PageWalk records what walking next_page_token actually did.
type PageWalk struct {
	Pages         int    `json:"pages"`
	Items         int    `json:"items"`
	DuplicateIDs  int    `json:"duplicate_ids"`
	RepeatedToken bool   `json:"repeated_token"`
	Stopped       string `json:"stopped"` // no_more | repeated_token | max_pages | error
	Err           string `json:"error,omitempty"`
}

// WalkPages follows next_page_token for at most maxPages pages. v1 relies on
// this for folder listing, so the probe checks for loops and duplicated items.
func (c *Client) WalkPages(ctx context.Context, limit, maxPages int) PageWalk {
	walk := PageWalk{}
	token := ""
	seenTokens := map[string]bool{}
	seenIDs := map[string]bool{}

	for walk.Pages < maxPages {
		page, err := c.ListFiles(ctx, "", ListOptions{Limit: limit, PageToken: token})
		if err != nil {
			walk.Stopped = "error"
			walk.Err = redactError(err)
			return walk
		}
		walk.Pages++
		for _, f := range page.Items {
			walk.Items++
			if f.ID != "" {
				if seenIDs[f.ID] {
					walk.DuplicateIDs++
				}
				seenIDs[f.ID] = true
			}
		}
		if page.NextPageToken == "" {
			walk.Stopped = "no_more"
			return walk
		}
		if seenTokens[page.NextPageToken] {
			walk.RepeatedToken = true
			walk.Stopped = "repeated_token"
			return walk
		}
		seenTokens[page.NextPageToken] = true
		token = page.NextPageToken
	}
	walk.Stopped = "max_pages"
	return walk
}

// BurstResult records how a short concurrent burst behaved: whether every
// request succeeded and whether the client throttle serialized them.
type BurstResult struct {
	Requests  int         `json:"requests"`
	OK        int         `json:"ok"`
	Failed    int         `json:"failed"`
	Statuses  map[int]int `json:"statuses"`
	ElapsedMS int64       `json:"elapsed_ms"`
	Err       string      `json:"error,omitempty"`
}

// Burst fires n concurrent list requests. It is small on purpose: v1 runs
// uploads and thumbnails concurrently, and this shows the API tolerates it.
func (c *Client) Burst(ctx context.Context, n int) BurstResult {
	res := BurstResult{Requests: n, Statuses: map[int]int{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.ListFiles(ctx, "", ListOptions{Limit: 1})
			status := http.StatusOK
			if err != nil {
				status = 0
				var apiErr *APIError
				if errors.As(err, &apiErr) {
					status = apiErr.Status
				}
				mu.Lock()
				if res.Err == "" {
					res.Err = redactError(err)
				}
				mu.Unlock()
			}
			mu.Lock()
			res.Statuses[status]++
			if err == nil {
				res.OK++
			} else {
				res.Failed++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	res.ElapsedMS = time.Since(start).Milliseconds()
	return res
}

func redactError(err error) string {
	return probe.Redact(err.Error())
}
