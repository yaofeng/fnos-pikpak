package httpapi

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
)

const (
	uploadProbeFileName = "pikpak-probe-20m.bin"
	uploadProbeSize     = int64(20 << 20)
)

func uploadProbeDir() string {
	if v := os.Getenv("TRIM_PKGTMP"); v != "" {
		return v
	}
	return filepath.Join(os.TempDir(), "pikpak-probe")
}

func uploadProbePath() string {
	return filepath.Join(uploadProbeDir(), uploadProbeFileName)
}

// ensureUploadProbeFile writes the deterministic 20 MiB probe payload. It is
// our own file, not user data, so it skips the fnOS ACL check.
func ensureUploadProbeFile() (string, error) {
	path := uploadProbePath()
	if st, err := os.Stat(path); err == nil && st.Size() == uploadProbeSize {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	block := make([]byte, 64<<10)
	for i := range block {
		block[i] = byte('a' + i%26)
	}
	for remaining := uploadProbeSize; remaining > 0; {
		n := int64(len(block))
		if remaining < n {
			n = remaining
		}
		if _, err := f.Write(block[:n]); err != nil {
			return "", err
		}
		remaining -= n
	}
	return path, nil
}

// ensureFreshProbeFile writes a 20 MiB payload with a GCID no account has seen,
// so RequestUpload has to accept a real transfer instead of answering with an
// instant upload. The unique name also keeps each run a separate file.
func ensureFreshProbeFile() (string, error) {
	dir := uploadProbeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "pikpak-probe-20m-*.bin")
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 64<<10)
	if _, err := rand.Read(head); err != nil {
		return "", err
	}
	if _, err := f.Write(head); err != nil {
		return "", err
	}

	block := make([]byte, 64<<10)
	for i := range block {
		block[i] = byte('a' + i%26)
	}
	written := int64(len(head))
	for written < uploadProbeSize {
		n := int64(len(block))
		if remaining := uploadProbeSize - written; remaining < n {
			n = remaining
		}
		if _, err := f.Write(block[:n]); err != nil {
			return "", err
		}
		written += n
	}
	return f.Name(), nil
}

func registerUploadRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/upload/probe/config", func(w http.ResponseWriter, r *http.Request) {
		if !deps.Debug {
			writeError(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		bound := false
		if _, err := deps.Sessions.Get(r.Context(), id.UID); err == nil {
			bound = true
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"generated_path": uploadProbePath(),
			"generated_size": uploadProbeSize,
			"bound":          bound,
		})
	})

	mux.HandleFunc("POST /api/upload/probe", func(w http.ResponseWriter, r *http.Request) {
		if !deps.Debug {
			writeError(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())

		var body struct {
			LocalPath string `json:"local_path"`
			ParentID  string `json:"parent_id"`
			// Fresh forces a payload with an unseen GCID, so the upload goes
			// through the provider instead of being deduplicated.
			Fresh bool `json:"fresh"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		if body.LocalPath == "" || !filepath.IsAbs(body.LocalPath) {
			writeError(w, http.StatusBadRequest, "bad_path", "需要一个绝对路径")
			return
		}

		path := filepath.Clean(body.LocalPath)
		if path == uploadProbePath() {
			var p string
			var err error
			if body.Fresh {
				p, err = ensureFreshProbeFile()
			} else {
				p, err = ensureUploadProbeFile()
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法生成探针文件")
				return
			}
			path = p
		} else {
			// The probe reads a whole file into an upload, so the path goes
			// through the same resolution as a queued upload: a link under a
			// granted folder must not reach a file outside it.
			resolved, _, err := resolveUploadSource(r, deps, id.UID, body.LocalPath)
			if err != nil {
				writeUploadSourceError(w, err)
				return
			}
			path = resolved
		}

		client, err := deps.Sessions.ClientFor(r.Context(), id.UID)
		if errors.Is(err, pikpak.ErrNotBound) {
			writeError(w, http.StatusUnauthorized, "not_bound", "请先绑定 PikPak 账号")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法加载 PikPak 会话")
			return
		}

		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			writeError(w, http.StatusBadRequest, "bad_path", "路径必须是可读的普通文件")
			return
		}

		ctx := r.Context()
		started := time.Now()

		gcidStart := time.Now()
		gcid, err := pikpak.GCIDFile(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "计算 GCID 失败")
			return
		}
		gcidMS := time.Since(gcidStart).Milliseconds()

		ticket, err := client.RequestUpload(ctx, pikpak.UploadRequest{
			Name:     filepath.Base(path),
			Size:     info.Size(),
			GCID:     gcid,
			ParentID: body.ParentID,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
			return
		}

		instant := ticket.Resumable == nil
		var uploadMS, waitMS int64
		if !instant {
			uploadStart := time.Now()
			f, err := os.Open(path)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法打开文件")
				return
			}
			err = client.CompleteUpload(ctx, ticket, f)
			_ = f.Close()
			if err != nil {
				writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
				return
			}
			uploadMS = time.Since(uploadStart).Milliseconds()
		}

		file := ticket.File
		if !instant && (file == nil || file.ID == "") {
			// The resumable response does not always include the file; look it
			// up so completion is still verified rather than assumed.
			found, err := findUploadedFile(r.Context(), client, body.ParentID, filepath.Base(path))
			if err != nil {
				writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
				return
			}
			file = found
		}
		if !instant && file != nil && file.ID != "" {
			waitStart := time.Now()
			done, err := client.WaitFileComplete(ctx, file.ID, 90*time.Second)
			waitMS = time.Since(waitStart).Milliseconds()
			if err != nil {
				writeError(w, http.StatusBadGateway, "pikpak_error", probe.Redact(err.Error()))
				return
			}
			file = &done
		}

		resp := map[string]any{
			"local_path": path,
			"size":       info.Size(),
			"gcid":       gcid,
			"gcid_ms":    gcidMS,
			"instant":    instant,
			"upload_ms":  uploadMS,
			"wait_ms":    waitMS,
			"total_ms":   time.Since(started).Milliseconds(),
		}
		if body.Fresh {
			// A fresh payload exists only for this probe; purge it so repeated
			// runs do not fill the account — locally as well as remotely.
			defer func() { _ = os.Remove(path) }()
			resp["fresh"] = true
			resp["deleted"] = false
			if file != nil && file.ID != "" {
				if err := client.DeletePermanently(ctx, []string{file.ID}); err != nil {
					resp["delete_error"] = probe.Redact(err.Error())
				} else {
					resp["deleted"] = true
				}
			}
		}
		if ticket.Resumable != nil {
			resp["provider"] = ticket.Resumable.Provider
			resp["param_keys"] = sortedParamKeys(ticket.Resumable.Params)
		}
		if file != nil {
			resp["file"] = map[string]any{
				"id":    file.ID,
				"name":  file.Name,
				"size":  file.Size,
				"phase": file.Phase,
			}
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

// findUploadedFile lists the target folder and matches by exact name.
func findUploadedFile(ctx context.Context, c *pikpak.Client, parentID, name string) (*pikpak.File, error) {
	page, err := c.ListFiles(ctx, parentID, pikpak.ListOptions{Limit: 100})
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].Name == name {
			return &page.Items[i], nil
		}
	}
	return nil, nil
}

func sortedParamKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
