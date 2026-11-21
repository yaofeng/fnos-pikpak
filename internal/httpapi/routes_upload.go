package httpapi

import (
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

func registerUploadRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/upload/probe/config", func(w http.ResponseWriter, r *http.Request) {
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
		id, _ := fnos.IdentityFrom(r.Context())

		var body struct {
			LocalPath string `json:"local_path"`
			ParentID  string `json:"parent_id"`
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
			p, err := ensureUploadProbeFile()
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法生成探针文件")
				return
			}
			path = p
		} else {
			if deps.Fnos == nil {
				writeError(w, http.StatusServiceUnavailable, "fnos_unavailable", "飞牛文件服务未配置")
				return
			}
			folders, err := deps.Fnos.GetUserAccessibleFolders(r.Context(), id.UID)
			if err != nil {
				writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
				return
			}
			if !fnos.IsUnderAny(path, folders) {
				writeError(w, http.StatusForbidden, "forbidden", "该路径不在当前用户的授权目录内")
				return
			}
			acl, err := deps.Fnos.CheckUserACL(r.Context(), id.UID, []string{path})
			if err != nil {
				writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
				return
			}
			if len(acl) != 1 || !acl[0].Readable {
				writeError(w, http.StatusForbidden, "forbidden", "当前用户没有读取该文件的权限")
				return
			}
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

func sortedParamKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
