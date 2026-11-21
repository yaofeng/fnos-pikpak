package pikpak

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// CreateFolder creates a folder under parentID ("" is the root).
func (c *Client) CreateFolder(ctx context.Context, parentID, name string) (File, error) {
	body := map[string]any{
		"kind": "drive#folder",
		"name": name,
	}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	var raw struct {
		File File `json:"file"`
	}
	if err := c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files", body, &raw); err != nil {
		return File{}, err
	}
	return raw.File, nil
}

func (c *Client) Rename(ctx context.Context, id, name string) error {
	return c.do(ctx, http.MethodPatch,
		c.BaseDrive+"/drive/v1/files/"+url.PathEscape(id),
		map[string]any{"name": name}, nil)
}

func (c *Client) FileDetail(ctx context.Context, id string) (File, error) {
	var f File
	err := c.do(ctx, http.MethodGet, c.BaseDrive+"/drive/v1/files/"+url.PathEscape(id), nil, &f)
	return f, err
}

// batchTask posts an ids list to one of the files:<op> endpoints and returns
// the task id PikPak reports (empty when the operation is synchronous).
func (c *Client) batchTask(ctx context.Context, op string, ids []string, extra map[string]any) (string, error) {
	body := map[string]any{"ids": ids}
	for k, v := range extra {
		body[k] = v
	}
	var raw struct {
		TaskID string `json:"task_id"`
	}
	if err := c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files:"+op, body, &raw); err != nil {
		return "", err
	}
	return raw.TaskID, nil
}

func (c *Client) BatchMove(ctx context.Context, ids []string, toParentID string) (string, error) {
	extra := map[string]any{}
	if toParentID != "" {
		extra["to"] = map[string]string{"parent_id": toParentID}
	}
	return c.batchTask(ctx, "batchMove", ids, extra)
}

func (c *Client) BatchCopy(ctx context.Context, ids []string, toParentID string) (string, error) {
	extra := map[string]any{}
	if toParentID != "" {
		extra["to"] = map[string]string{"parent_id": toParentID}
	}
	return c.batchTask(ctx, "batchCopy", ids, extra)
}

func (c *Client) BatchTrash(ctx context.Context, ids []string) (string, error) {
	return c.batchTask(ctx, "batchTrash", ids, nil)
}

func (c *Client) BatchUntrash(ctx context.Context, ids []string) (string, error) {
	return c.batchTask(ctx, "batchUntrash", ids, nil)
}

// BatchDelete purges files permanently, skipping the trash.
func (c *Client) BatchDelete(ctx context.Context, ids []string) (string, error) {
	return c.batchTask(ctx, "batchDelete", ids, nil)
}

func (c *Client) Star(ctx context.Context, ids []string) error {
	return c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files:star", map[string]any{"ids": ids}, nil)
}

func (c *Client) Unstar(ctx context.Context, ids []string) error {
	return c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files:unstar", map[string]any{"ids": ids}, nil)
}

// EmptyTrash purges everything in the trash and returns the (possibly empty)
// task id.
func (c *Client) EmptyTrash(ctx context.Context) (string, error) {
	var raw struct {
		TaskID string `json:"task_id"`
	}
	err := c.do(ctx, http.MethodPatch, c.BaseDrive+"/drive/v1/files/trash:empty", nil, &raw)
	return raw.TaskID, err
}

// WaitTaskSettled waits for a batch task to stop running. It returns the last
// observed task and whether it completed; a timeout or a still-running task
// is reported as done=false with no error, so the HTTP layer can answer
// "pending" instead of pretending the operation finished.
func (c *Client) WaitTaskSettled(ctx context.Context, id string, timeout, interval time.Duration) (Task, bool, error) {
	deadline := time.Now().Add(timeout)
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	var last Task
	for {
		task, err := c.GetTask(ctx, id)
		if err != nil {
			return last, false, err
		}
		last = task
		switch task.Phase {
		case "PHASE_TYPE_COMPLETE":
			return task, true, nil
		case "PHASE_TYPE_ERROR":
			return task, false, nil
		}
		if time.Now().After(deadline) {
			return task, false, nil
		}
		select {
		case <-ctx.Done():
			return task, false, ctx.Err()
		case <-time.After(interval):
		}
	}
}
