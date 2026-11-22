package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
	"fnos-pikpak/internal/transfer"
)

var (
	ErrDirForbidden   = errors.New("httpapi: path is not writable by this user")
	ErrDirUnavailable = errors.New("httpapi: fnOS file service is unavailable")
)

// DirGuard answers whether uid may read/write dir. It isolates the fnOS ACL
// dependency, which cannot run on a development machine.
type DirGuard func(ctx context.Context, uid int, dir string, needWrite bool) error

// FnosDirGuard is the production directory guard: the folder must be one fnOS
// lists for the user and the ACL must allow the requested access.
func FnosDirGuard(c *fnos.Client) DirGuard { return fnosDirGuard(c) }

func fnosDirGuard(c *fnos.Client) DirGuard {
	return func(ctx context.Context, uid int, dir string, needWrite bool) error {
		if c == nil {
			return ErrDirUnavailable
		}
		folders, err := c.GetUserAccessibleFolders(ctx, uid)
		if err != nil {
			return err
		}
		if !fnos.IsUnderAny(dir, folders) {
			return ErrDirForbidden
		}
		acl, err := c.CheckUserACL(ctx, uid, []string{dir})
		if err != nil {
			return err
		}
		if len(acl) != 1 || !acl[0].Readable || (needWrite && !acl[0].Writable) {
			return ErrDirForbidden
		}
		return nil
	}
}

// DevDirGuard is the DirGuard used by --dev-uid runs: there is no fnOS gateway
// on a development machine to answer ACL questions, so every absolute path is
// allowed. Production always goes through FnosDirGuard.
func DevDirGuard() DirGuard {
	return func(_ context.Context, _ int, dir string, _ bool) error {
		if !filepath.IsAbs(dir) {
			return ErrDirForbidden
		}
		return nil
	}
}

func registerTransferRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/downloads", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		var statuses []string
		if raw := r.URL.Query().Get("status"); raw != "" {
			statuses = strings.Split(raw, ",")
		}
		jobs, err := deps.Store.ListDownloadJobs(r.Context(), id.UID, statuses...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取下载任务")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": jobs})
	})

	mux.HandleFunc("POST /api/downloads", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			FileIDs  []string `json:"file_ids"`
			DestDir  string   `json:"dest_dir"`
			Conflict string   `json:"conflict"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if len(body.FileIDs) == 0 || len(body.FileIDs) > maxBatchIDs {
			writeError(w, http.StatusBadRequest, "bad_ids", "一次需要选择 1 到 200 个文件")
			return
		}
		destDir := strings.TrimSpace(body.DestDir)
		if destDir == "" {
			// The client sends no directory any more: downloads land in the
			// folder the user configured once, in settings.
			destDir = downloadDir(r, deps.Store, id.UID)
			if destDir == "" {
				writeError(w, http.StatusBadRequest, "download_dir_unset", "请先在设置中指定默认下载目录")
				return
			}
		}
		destDir = filepath.Clean(destDir)
		if !filepath.IsAbs(destDir) {
			writeError(w, http.StatusBadRequest, "bad_path", "下载目录必须是绝对路径")
			return
		}
		if err := checkDir(r, deps, id.UID, destDir, true); err != nil {
			writeDirError(w, err)
			return
		}
		client, ok := boundClient(w, r, deps)
		if !ok {
			return
		}

		policy := transfer.ConflictPolicy(strings.TrimSpace(body.Conflict))
		if policy == "" {
			policy = conflictPolicy(r.Context(), deps.Store, id.UID)
		}

		created := []store.DownloadJob{}
		skipped := []string{}
		for _, fileID := range body.FileIDs {
			file, err := client.FileDetail(r.Context(), fileID)
			if err != nil {
				writeUpstreamError(w, err)
				return
			}
			name := filepath.Base(strings.TrimSpace(file.Name))
			if name == "" || name == "." || name == "/" {
				continue
			}
			destPath := filepath.Join(destDir, name)
			switch policy {
			case transfer.PolicySkip:
				if _, err := os.Stat(destPath); err == nil {
					skipped = append(skipped, name)
					continue
				}
			case transfer.PolicyOverwrite:
				// keep the name; the worker replaces the file atomically
			default:
				destPath = uniqueDestPath(destPath)
			}

			job := store.DownloadJob{
				ID: newJobID(), UID: id.UID, FileID: file.ID, FileName: name,
				DestDir: destDir, DestPath: destPath, Size: file.Size, Status: "queued",
			}
			if err := deps.Store.PutDownloadJob(r.Context(), job); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法创建下载任务")
				return
			}
			// Read the row back so the response carries the stored timestamps.
			if stored, err := deps.Store.GetDownloadJob(r.Context(), id.UID, job.ID); err == nil {
				job = stored
			}
			created = append(created, job)
		}
		notifyQueue(deps)
		writeJSON(w, http.StatusCreated, map[string]any{"created": created, "skipped": skipped})
	})

	registerTransferControl(mux, deps, "downloads", func(uid int, jobID, action string) bool {
		return controlDownload(deps, uid, jobID, action)
	})

	mux.HandleFunc("DELETE /api/downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		job, err := deps.Store.GetDownloadJob(r.Context(), id.UID, r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "任务不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取任务")
			return
		}
		cancelRunning(deps, job.ID)
		if r.URL.Query().Get("delete_file") == "true" && job.Status != "running" {
			_ = os.Remove(job.DestPath)
			_ = os.Remove(job.DestPath + ".pikpak-part")
		}
		if err := deps.Store.DeleteDownloadJob(r.Context(), id.UID, job.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法删除任务")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("GET /api/uploads", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		jobs, err := deps.Store.ListUploadJobs(r.Context(), id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取上传任务")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": jobs})
	})

	mux.HandleFunc("POST /api/uploads", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			LocalPaths []string `json:"local_paths"`
			ParentID   string   `json:"parent_id"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if len(body.LocalPaths) == 0 || len(body.LocalPaths) > maxBatchIDs {
			writeError(w, http.StatusBadRequest, "bad_paths", "一次需要选择 1 到 200 个文件")
			return
		}
		if _, ok := boundClient(w, r, deps); !ok {
			return
		}

		created := []store.UploadJob{}
		for _, raw := range body.LocalPaths {
			path := filepath.Clean(strings.TrimSpace(raw))
			if !filepath.IsAbs(path) {
				writeError(w, http.StatusBadRequest, "bad_path", "上传源必须是绝对路径")
				return
			}
			if err := checkDir(r, deps, id.UID, filepath.Dir(path), false); err != nil {
				writeDirError(w, err)
				return
			}
			st, err := os.Stat(path)
			if err != nil || !st.Mode().IsRegular() {
				writeError(w, http.StatusBadRequest, "bad_path", "上传源必须是可读的普通文件")
				return
			}
			job := store.UploadJob{
				ID: newJobID(), UID: id.UID, LocalPath: path,
				ParentID: strings.TrimSpace(body.ParentID), RemoteName: filepath.Base(path),
				Size: st.Size(), Status: "queued",
			}
			if err := deps.Store.PutUploadJob(r.Context(), job); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法创建上传任务")
				return
			}
			if stored, err := deps.Store.GetUploadJob(r.Context(), id.UID, job.ID); err == nil {
				job = stored
			}
			created = append(created, job)
		}
		notifyQueue(deps)
		writeJSON(w, http.StatusCreated, map[string]any{"created": created})
	})

	registerTransferControl(mux, deps, "uploads", func(uid int, jobID, action string) bool {
		return controlUpload(deps, uid, jobID, action)
	})

	mux.HandleFunc("DELETE /api/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		if _, err := deps.Store.GetUploadJob(r.Context(), id.UID, r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "任务不存在")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取任务")
			return
		}
		cancelRunning(deps, r.PathValue("id"))
		if err := deps.Store.DeleteUploadJob(r.Context(), id.UID, r.PathValue("id")); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法删除任务")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

// registerTransferControl wires pause/resume/cancel and DELETE for one kind.
func registerTransferControl(mux *http.ServeMux, deps Deps, kind string, apply func(uid int, jobID, action string) bool) {
	mux.HandleFunc("POST /api/"+kind+"/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		action := r.PathValue("action")
		switch action {
		case "pause", "resume", "cancel":
		default:
			writeError(w, http.StatusNotFound, "not_found", "未知操作")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		if !apply(id.UID, r.PathValue("id"), action) {
			writeError(w, http.StatusNotFound, "not_found", "任务不存在")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func controlDownload(deps Deps, uid int, jobID, action string) bool {
	ctx := context.Background()
	job, err := deps.Store.GetDownloadJob(ctx, uid, jobID)
	if err != nil {
		return false
	}
	switch action {
	case "pause":
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "paused"
			if err := deps.Store.PutDownloadJob(ctx, job); err != nil {
				return false
			}
			cancelRunning(deps, jobID)
		}
	case "resume":
		if job.Status == "paused" || job.Status == "failed" || job.Status == "canceled" {
			job.Status = "queued"
			job.ErrorCode = ""
			job.ErrorMessage = ""
			if err := deps.Store.PutDownloadJob(ctx, job); err != nil {
				return false
			}
			notifyQueue(deps)
		}
	case "cancel":
		if job.Status != "done" {
			job.Status = "canceled"
			if err := deps.Store.PutDownloadJob(ctx, job); err != nil {
				return false
			}
			cancelRunning(deps, jobID)
			_ = os.Remove(job.DestPath + ".pikpak-part")
		}
	}
	return true
}

func controlUpload(deps Deps, uid int, jobID, action string) bool {
	ctx := context.Background()
	job, err := deps.Store.GetUploadJob(ctx, uid, jobID)
	if err != nil {
		return false
	}
	switch action {
	case "pause":
		if job.Status == "queued" || job.Status == "running" {
			job.Status = "paused"
			if err := deps.Store.PutUploadJob(ctx, job); err != nil {
				return false
			}
			cancelRunning(deps, jobID)
		}
	case "resume":
		// Uploads restart from the beginning; PikPak's resumable protocol is
		// driven by the client and a paused file has no live session.
		if job.Status == "paused" || job.Status == "failed" || job.Status == "canceled" {
			job.Status = "queued"
			job.ErrorCode = ""
			job.ErrorMessage = ""
			if err := deps.Store.PutUploadJob(ctx, job); err != nil {
				return false
			}
			notifyQueue(deps)
		}
	case "cancel":
		if job.Status != "done" {
			job.Status = "canceled"
			if err := deps.Store.PutUploadJob(ctx, job); err != nil {
				return false
			}
			cancelRunning(deps, jobID)
		}
	}
	return true
}

func cancelRunning(deps Deps, jobID string) {
	if deps.Queue != nil {
		deps.Queue.CancelRunning(jobID)
	}
}

func notifyQueue(deps Deps) {
	if deps.Queue != nil {
		deps.Queue.Notify()
	}
}

func checkDir(r *http.Request, deps Deps, uid int, dir string, needWrite bool) error {
	guard := deps.DirGuard
	if guard == nil {
		guard = fnosDirGuard(deps.Fnos)
	}
	err := guard(r.Context(), uid, dir, needWrite)
	if errors.Is(err, ErrDirUnavailable) {
		return ErrDirUnavailable
	}
	if err != nil {
		// ACL failures and Open API failures both mean "not allowed to write
		// here" from the user's point of view; the message distinguishes them.
		return err
	}
	return nil
}

func writeDirError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDirUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "fnos_unavailable", "飞牛文件服务未配置")
		return
	}
	if errors.Is(err, ErrDirForbidden) {
		writeError(w, http.StatusForbidden, "forbidden", "该目录不在当前用户的授权范围内")
		return
	}
	writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
}

// downloadDir returns the user's configured download folder, falling back to
// the global one. Empty means nothing has been configured yet.
func downloadDir(r *http.Request, st *store.Store, uid int) string {
	if v, err := st.GetUserSetting(r.Context(), uid, "download_dir"); err == nil && v != "" {
		return v
	}
	if v, err := st.GetSetting(r.Context(), "download_dir"); err == nil && v != "" {
		return v
	}
	return ""
}

func conflictPolicy(ctx context.Context, st *store.Store, uid int) transfer.ConflictPolicy {
	value := ""
	if v, err := st.GetUserSetting(ctx, uid, "conflict"); err == nil {
		value = v
	} else if v, err := st.GetSetting(ctx, "conflict"); err == nil {
		value = v
	}
	switch transfer.ConflictPolicy(value) {
	case transfer.PolicySkip, transfer.PolicyOverwrite:
		return transfer.ConflictPolicy(value)
	default:
		return transfer.PolicyRename
	}
}

func uniqueDestPath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; i < 1000; i++ {
		candidate := base + " (" + itoa(i) + ")" + ext
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return base + " (" + itoa(int(time.Now().UnixNano())) + ")" + ext
}

func itoa(n int) string { return strconv.Itoa(n) }

func newJobID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
