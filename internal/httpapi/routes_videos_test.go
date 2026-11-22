package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"fnos-pikpak/internal/store"
)

// videoUpstream emulates the PikPak drive calls the prepare worker makes: the
// quota, the folders it walks, restore, purge and move. Every mutation is
// recorded so a test can assert exactly what the worker did.
type videoUpstream struct {
	mu sync.Mutex

	limit int64
	usage int64

	// folders maps a parent id ("" is the drive root) to its children.
	folders map[string][]map[string]any
	// restored is the file a share restore drops into.
	restored map[string]any
	// restoreParent names the folder a restore drops into ("" is the root).
	// PikPak normally uses the root folder "Pack From Shared".
	restoreParent string
	// badPassCode makes /drive/v1/share fail the way PikPak does.
	badPassCode bool

	deleted    []string
	moved      []string
	restores   int
	aboutCalls int
}

func fileEntry(id, name string, size int64, modified string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "kind": "drive#file",
		"size": size, "modified_time": modified,
	}
}

func folderEntry(id, name string) map[string]any {
	return map[string]any{"id": id, "name": name, "kind": "drive#folder"}
}

func newVideoUpstream() *videoUpstream {
	return &videoUpstream{
		limit:    1000,
		usage:    900,
		folders:  map[string][]map[string]any{},
		restored: fileEntry("df1", "new.mp4", 50, "2026-11-20T10:00:00Z"),
	}
}

func (u *videoUpstream) set(parent string, items ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.folders[parent] = items
}

func (u *videoUpstream) children(parent string) []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	items := u.folders[parent]
	if items == nil {
		return []map[string]any{}
	}
	return items
}

// take removes one entry from any folder, returning the bytes it occupied.
func (u *videoUpstream) take(id string) (int64, bool) {
	for parent, items := range u.folders {
		for i, item := range items {
			if item["id"] != id {
				continue
			}
			size, _ := item["size"].(int64)
			u.folders[parent] = append(items[:i:i], items[i+1:]...)
			return size, true
		}
	}
	return 0, false
}

func (u *videoUpstream) snapshot() (deleted, moved []string, restores, aboutCalls int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.deleted...), append([]string(nil), u.moved...), u.restores, u.aboutCalls
}

func (u *videoUpstream) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/drive/v1/about", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.aboutCalls++
		body := map[string]any{"quota": map[string]any{
			"limit": jsonInt(u.limit), "usage": jsonInt(u.usage),
		}}
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(body)
	})

	mux.HandleFunc("/drive/v1/share", func(w http.ResponseWriter, r *http.Request) {
		pass := r.URL.Query().Get("pass_code")
		status := "OK"
		if u.badPassCode && pass != "7f3a" {
			status = "PASS_CODE_ERROR"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"share_status":    status,
			"pass_code_token": "tok-1",
			"title":           "资料",
			"share_creator":   map[string]any{"name": "张三"},
			"files":           []map[string]any{fileEntry("sf2", "new.mp4", 50, "2026-11-01T00:00:00Z")},
		})
	})

	mux.HandleFunc("/drive/v1/share/file_info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"file_info": map[string]any{
			"id": r.URL.Query().Get("file_id"), "name": "new.mp4",
			"mime_type": "video/mp4", "size": "50",
			"web_content_link": "https://cdn.example/new.mp4",
		}})
	})

	mux.HandleFunc("/drive/v1/share/restore", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.restores++
		parent := u.restoreParent
		if parent != "" {
			if _, ok := u.folders[parent]; !ok {
				u.folders[""] = append(u.folders[""], folderEntry(parent, packFromSharedName))
				u.folders[parent] = []map[string]any{}
			}
		}
		u.folders[parent] = append(u.folders[parent], u.restored)
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "rt1"})
	})

	mux.HandleFunc("/drive/v1/tasks/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"phase": "PHASE_TYPE_COMPLETE"})
	})

	mux.HandleFunc("/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body struct {
				Kind     string `json:"kind"`
				Name     string `json:"name"`
				ParentID string `json:"parent_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			u.mu.Lock()
			folder := folderEntry("made-"+body.Name, body.Name)
			u.folders[body.ParentID] = append(u.folders[body.ParentID], folder)
			u.folders["made-"+body.Name] = []map[string]any{}
			u.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"file": folder})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files":           u.children(r.URL.Query().Get("parent_id")),
			"next_page_token": "",
		})
	})

	mux.HandleFunc("/drive/v1/files:batchDelete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		u.mu.Lock()
		for _, id := range body.IDs {
			if size, ok := u.take(id); ok {
				u.usage -= size
				u.deleted = append(u.deleted, id)
			}
		}
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "d1"})
	})

	mux.HandleFunc("/drive/v1/files:batchMove", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
			To  struct {
				ParentID string `json:"parent_id"`
			} `json:"to"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		u.mu.Lock()
		for _, id := range body.IDs {
		find:
			for parent, items := range u.folders {
				for i, item := range items {
					if item["id"] != id {
						continue
					}
					u.folders[parent] = append(items[:i:i], items[i+1:]...)
					moved := map[string]any{}
					for k, v := range item {
						moved[k] = v
					}
					moved["parent_id"] = body.To.ParentID
					u.folders[body.To.ParentID] = append(u.folders[body.To.ParentID], moved)
					u.moved = append(u.moved, id)
					break find
				}
			}
		}
		u.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"task_id": "m1"})
	})

	return mux
}

// jsonInt formats a number the way PikPak sends quota values: as a string.
func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func newVideoEnv(t *testing.T, tweak func(up *videoUpstream)) (Deps, http.Handler, *videoUpstream) {
	t.Helper()
	up := newVideoUpstream()
	if tweak != nil {
		tweak(up)
	}
	srv := httptest.NewServer(up.handler())
	t.Cleanup(srv.Close)
	deps, st := newAuthTestDeps(t, srv.URL)
	bindTestUser(t, st, 1000)
	deps.ShareSessions = newShareSessions()
	deps.Videos = newVideoJobs()
	deps.ShareSessions.put(1000, "VOabc123", "tok-1", time.Now())
	return deps, NewMux(deps), up
}

// readyVideoJob posts a prepare request and polls until the worker settles.
func readyVideoJob(t *testing.T, h http.Handler, body string) map[string]any {
	t.Helper()
	rec, out := postJSON(t, h, "/api/videos/prepare", "1000", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("prepare status = %d body=%v", rec.Code, out)
	}
	id, _ := out["job_id"].(string)
	if id == "" {
		t.Fatalf("no job id in %v", out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec, job := getJSON(t, h, "/api/videos/jobs/"+id, "1000")
		if rec.Code != http.StatusOK {
			t.Fatalf("job status = %d body=%v", rec.Code, job)
		}
		if job["state"] == "ready" || job["state"] == "failed" {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s never settled", id)
	return nil
}

func TestPrepareJobRunsInTheBackground(t *testing.T) {
	jobs := newVideoJobs()
	release := make(chan struct{})
	id, ok := jobs.Start(1000, func(ctx context.Context) (videoPrepareResult, error) {
		<-release
		return videoPrepareResult{FileID: "df1", Name: "new.mp4", Size: 50, StreamURL: "/api/stream/df1?t=tok"}, nil
	})
	if !ok || id == "" {
		t.Fatalf("start = %q %v", id, ok)
	}
	job, found := jobs.Get(1000, id, time.Now())
	if !found || job.State != videoJobRunning {
		t.Fatalf("running job = %+v found=%v", job, found)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, _ = jobs.Get(1000, id, time.Now())
		if job.State == videoJobReady {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if job.State != videoJobReady || job.StreamURL != "/api/stream/df1?t=tok" || job.Size != 50 {
		t.Fatalf("settled job = %+v", job)
	}
	if _, other := jobs.Get(2000, id, time.Now()); other {
		t.Fatal("another user read the job")
	}
}

func TestPrepareRefusesASecondJobForTheSameUser(t *testing.T) {
	deps, h, _ := newVideoEnv(t, nil)
	block := make(chan struct{})
	if _, ok := deps.Videos.Start(1000, func(ctx context.Context) (videoPrepareResult, error) {
		<-block
		return videoPrepareResult{}, nil
	}); !ok {
		t.Fatal("the first job was refused")
	}
	defer close(block)

	rec, out := postJSON(t, h, "/api/videos/prepare", "1000", `{"share_id":"VOabc123","file_id":"sf2"}`)
	if rec.Code != http.StatusConflict || errCode(out) != "video_busy" {
		t.Fatalf("second prepare = %d %v", rec.Code, out)
	}
}

func TestPrepareRestoresAndMovesToTemp(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"), fileEntry("old0", "old.bin", 10, "2026-10-01T00:00:00Z"))
		up.set("f1", folderEntry("f3", "临时"))
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if url, _ := job["stream_url"].(string); !strings.HasPrefix(url, "/api/stream/df1?t=") {
		t.Fatalf("stream url = %v", job["stream_url"])
	}
	_, moved, restores, _ := up.snapshot()
	if restores != 1 || len(moved) != 1 || moved[0] != "df1" {
		t.Fatalf("restores=%d moved=%v", restores, moved)
	}
	if temp := up.children("f3"); len(temp) != 1 || temp[0]["id"] != "df1" {
		t.Fatalf("临时 = %v", temp)
	}
	if deleted, _, _, _ := up.snapshot(); len(deleted) != 0 {
		t.Fatalf("deleted %v with enough space", deleted)
	}
}

func TestPrepareDeletesOldestUntilQuotaFits(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.usage = 990
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"))
		up.set("f1", folderEntry("f3", "临时"))
		up.set("f3",
			fileEntry("old1", "old1.mkv", 60, "2026-10-01T00:00:00Z"),
			fileEntry("old2", "old2.mkv", 20, "2026-10-05T00:00:00Z"),
		)
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if deleted, _, _, _ := up.snapshot(); len(deleted) != 1 || deleted[0] != "old1" {
		t.Fatalf("deleted = %v, want only the oldest", deleted)
	}
}

func TestPrepareFallsBackToPackFromShared(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.usage = 995
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"))
		up.set("f1", folderEntry("f3", "临时"))
		up.set("f3", fileEntry("t1", "t1.mkv", 10, "2026-10-01T00:00:00Z"))
		up.set("f2",
			fileEntry("p1", "p1.mkv", 80, "2026-10-02T00:00:00Z"),
			fileEntry("p2", "p2.mkv", 5, "2026-10-03T00:00:00Z"),
		)
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	deleted, _, _, _ := up.snapshot()
	if len(deleted) != 2 || deleted[0] != "t1" || deleted[1] != "p1" {
		t.Fatalf("deleted = %v, want 临时 first then the oldest in Pack From Shared", deleted)
	}
}

func TestPrepareFailsWhenSpaceStillShort(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.usage = 995
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"))
		up.set("f1", folderEntry("f3", "临时"))
		up.set("f2", fileEntry("p9", "p9.mkv", 10, "2026-10-02T00:00:00Z"))
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "failed" {
		t.Fatalf("job = %v", job)
	}
	msg, _ := job["error"].(string)
	if !strings.Contains(msg, "空间不足") {
		t.Fatalf("error = %q", msg)
	}
	if _, _, restores, _ := up.snapshot(); restores != 0 {
		t.Fatalf("restored %d times without space", restores)
	}
}

func TestPreparePicksTheNewRootFileNotTheOldOne(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"),
			fileEntry("df0", "new.mp4", 50, "2026-09-01T00:00:00Z"))
		up.set("f1", folderEntry("f3", "临时"))
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if _, moved, _, _ := up.snapshot(); len(moved) != 1 || moved[0] != "df1" {
		t.Fatalf("moved = %v, want the freshly restored file", moved)
	}
}

func TestPreviewReturnsASignedShareURL(t *testing.T) {
	deps, h, _ := newVideoEnv(t, nil)
	fav, err := deps.Store.AddVideoFavorite(t.Context(), 1000, store.VideoFavorite{
		ShareID: "VOabc123", FileID: "sf2", Name: "new.mp4", Size: 50, PassCode: "7f3a",
	})
	if err != nil {
		t.Fatalf("AddVideoFavorite: %v", err)
	}

	rec, body := postJSON(t, h, "/api/videos/preview", "1000",
		`{"video_favorite_id":`+jsonInt(fav.ID)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%v", rec.Code, body)
	}
	stream, _ := body["stream_url"].(string)
	if !strings.HasPrefix(stream, "/api/share/stream/VOabc123/sf2?t=") {
		t.Fatalf("stream_url = %q", stream)
	}
	if strings.Contains(rec.Body.String(), "7f3a") {
		t.Fatal("the pass code reached the client")
	}
}

func TestPrepareFailsWhenTheSavedPassCodeIsWrong(t *testing.T) {
	deps, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.badPassCode = true
		up.set("", folderEntry("f1", "我的视频"), folderEntry("f2", "Pack From Shared"))
		up.set("f1", folderEntry("f3", "临时"))
	})
	fav, err := deps.Store.AddVideoFavorite(t.Context(), 1000, store.VideoFavorite{
		ShareID: "VOabc123", FileID: "sf2", Name: "new.mp4", Size: 50, PassCode: "wrong",
	})
	if err != nil {
		t.Fatalf("AddVideoFavorite: %v", err)
	}

	job := readyVideoJob(t, h, `{"video_favorite_id":`+jsonInt(fav.ID)+`}`)
	if job["state"] != "failed" {
		t.Fatalf("job = %v", job)
	}
	msg, _ := job["error"].(string)
	if !strings.Contains(msg, "密码") {
		t.Fatalf("error = %q", msg)
	}
	if _, _, restores, _ := up.snapshot(); restores != 0 {
		t.Fatalf("restored with a bad pass code %d times", restores)
	}
}

// PikPak restores a share into the root folder Pack From Shared (creating it
// on the first restore), not directly into the root.
func TestPrepareFindsTheVideoInsidePackFromShared(t *testing.T) {
	_, h, up := newVideoEnv(t, func(up *videoUpstream) {
		up.set("", folderEntry("f1", "我的视频"))
		up.set("f1", folderEntry("f3", "临时"))
		up.restoreParent = "pack1"
	})

	job := readyVideoJob(t, h, `{"share_id":"VOabc123","file_id":"sf2"}`)
	if job["state"] != "ready" {
		t.Fatalf("job = %v", job)
	}
	if url, _ := job["stream_url"].(string); !strings.HasPrefix(url, "/api/stream/df1?t=") {
		t.Fatalf("stream url = %v", job["stream_url"])
	}
	_, moved, _, _ := up.snapshot()
	if len(moved) != 1 || moved[0] != "df1" {
		t.Fatalf("moved = %v", moved)
	}
	if temp := up.children("f3"); len(temp) != 1 || temp[0]["id"] != "df1" {
		t.Fatalf("temp folder = %v", temp)
	}
}
