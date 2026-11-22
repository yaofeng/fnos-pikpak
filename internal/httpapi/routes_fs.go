package httpapi

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/probe"
)

type fsEntry struct {
	Name     string `json:"name"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	Modified string `json:"modified,omitempty"`
}

func registerFSProbe(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("POST /api/fs/probe-acl", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())

		var body struct {
			Paths    []string `json:"paths"`
			Language string   `json:"language"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		if len(body.Paths) == 0 {
			writeError(w, http.StatusBadRequest, "no_paths", "至少需要一个路径")
			return
		}
		if deps.Fnos == nil {
			writeError(w, http.StatusServiceUnavailable, "fnos_unavailable", "飞牛文件服务未配置")
			return
		}

		ctx := r.Context()
		folders, err := deps.Fnos.GetUserAccessibleFolders(ctx, id.UID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}
		acl, err := deps.Fnos.CheckUserACL(ctx, id.UID, body.Paths)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}

		lang := body.Language
		if lang == "" {
			lang = "zh-CN"
		}
		semantic, semanticErr := deps.Fnos.ConvertPath(ctx, body.Paths, lang)

		under := make(map[string]bool, len(body.Paths))
		for _, p := range body.Paths {
			under[p] = fnos.IsUnderAny(p, folders)
		}

		writableErr := fnos.ValidateWritableACL(acl)

		resp := map[string]any{
			"folders":  folders,
			"acl":      acl,
			"semantic": semantic,
			"under":    under,
		}
		if semanticErr != nil {
			resp["semantic_error"] = semanticErr.Error()
		}
		resp["writable_ok"] = writableErr == nil
		if writableErr != nil {
			resp["writable_error"] = writableErr.Error()
		}
		writeJSON(w, http.StatusOK, resp)
	})
}

func registerFSFolderRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/fs/folders", func(w http.ResponseWriter, r *http.Request) {
		if !requireFnos(w, deps) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		ctx := r.Context()

		live, err := deps.Fnos.GetUserAccessibleFolders(ctx, id.UID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}
		roots := unionFolders(ctx, deps, id.UID, live)

		semantic := map[string]string{}
		if converted, err := deps.Fnos.ConvertPath(ctx, roots, requestLang(r)); err == nil {
			for _, c := range converted {
				semantic[c.Path] = c.SemanticPath
			}
		}

		items := make([]map[string]any, 0, len(roots))
		for _, p := range roots {
			items = append(items, map[string]any{"path": p, "semantic_path": semantic[p]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	})

	mux.HandleFunc("GET /api/fs/list", func(w http.ResponseWriter, r *http.Request) {
		if !requireFnos(w, deps) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		dir, ok := readFSPath(w, r.URL.Query().Get("path"))
		if !ok {
			return
		}
		roots, ok := fsRoots(w, r, deps, id.UID)
		if !ok {
			return
		}
		if !fnos.IsUnderAny(dir, roots) {
			writeError(w, http.StatusForbidden, "forbidden", "该目录不在当前用户的授权范围内")
			return
		}

		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "not_found", "目录不存在")
				return
			}
			writeFSError(w, err)
			return
		}
		if !info.IsDir() {
			writeError(w, http.StatusBadRequest, "bad_path", "路径不是目录")
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			writeFSError(w, err)
			return
		}

		items := make([]fsEntry, 0, len(entries))
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			// A symlink inside a granted folder may point outside it; such an
			// entry is not listed at all rather than exposed as an error.
			if !fnos.IsUnderAny(full, roots) {
				continue
			}
			child, err := e.Info()
			if err != nil {
				continue
			}
			entry := fsEntry{
				Name:  e.Name(),
				IsDir: child.IsDir(),
				Size:  child.Size(),
			}
			if !child.ModTime().IsZero() {
				entry.Modified = child.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00")
			}
			items = append(items, entry)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].IsDir != items[j].IsDir {
				return items[i].IsDir
			}
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		})
		writeJSON(w, http.StatusOK, map[string]any{"path": dir, "items": items})
	})

	mux.HandleFunc("GET /api/fs/check", func(w http.ResponseWriter, r *http.Request) {
		if !requireFnos(w, deps) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		dir, ok := readFSPath(w, r.URL.Query().Get("path"))
		if !ok {
			return
		}
		need, ok := readNeed(w, r)
		if !ok {
			return
		}
		acl, err := deps.Fnos.CheckUserACL(r.Context(), id.UID, []string{dir})
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}
		res := fnos.ACLResult{Path: dir}
		if len(acl) > 0 && acl[0].Path == dir {
			res = acl[0]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":      dir,
			"readable":  res.Readable,
			"writable":  res.Writable,
			"deletable": res.Deletable,
			"need":      need,
			"ok":        aclSatisfies(res, need),
		})
	})

	mux.HandleFunc("POST /api/fs/authorize", func(w http.ResponseWriter, r *http.Request) {
		if !requireFnos(w, deps) {
			return
		}
		var body struct {
			Path string `json:"path"`
			Need string `json:"need"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			return
		}
		dir, ok := readFSPath(w, body.Path)
		if !ok {
			return
		}
		need := strings.TrimSpace(body.Need)
		if need == "" {
			need = "read"
		}
		if need != "read" && need != "write" {
			writeError(w, http.StatusBadRequest, "bad_need", "need 只能是 read 或 write")
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		ctx := r.Context()

		acl, err := deps.Fnos.CheckUserACL(ctx, id.UID, []string{dir})
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}
		res := fnos.ACLResult{Path: dir}
		if len(acl) > 0 && acl[0].Path == dir {
			res = acl[0]
		}
		if !aclSatisfies(res, need) {
			writeError(w, http.StatusForbidden, "forbidden", "当前用户没有该目录的权限")
			return
		}

		roots, err := deps.Fnos.GetUserAccessibleFolders(ctx, id.UID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
			return
		}
		// A folder the user just picked may not be in the grant list yet; the
		// ACL check above is the authority, so cache it either way.
		under := fnos.IsUnderAny(dir, roots)

		if deps.Store != nil {
			if err := deps.Store.GrantFolder(ctx, id.UID, dir, ""); err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "无法保存授权目录")
				return
			}
		}

		semanticPath := ""
		if converted, err := deps.Fnos.ConvertPath(ctx, []string{dir}, requestLang(r)); err == nil && len(converted) > 0 {
			semanticPath = converted[0].SemanticPath
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"path":          dir,
			"semantic_path": semanticPath,
			"under":         under,
			"readable":      res.Readable,
			"writable":      res.Writable,
		})
	})
}

func requireFnos(w http.ResponseWriter, deps Deps) bool {
	if deps.Fnos == nil {
		writeError(w, http.StatusServiceUnavailable, "fnos_unavailable", "飞牛文件服务未配置")
		return false
	}
	return true
}

func fsRoots(w http.ResponseWriter, r *http.Request, deps Deps, uid int) ([]string, bool) {
	live, err := deps.Fnos.GetUserAccessibleFolders(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
		return nil, false
	}
	return unionFolders(r.Context(), deps, uid, live), true
}

// unionFolders merges the live grant list with the cached one so a folder the
// user just authorized is usable before fnOS finishes syncing the list.
func unionFolders(ctx context.Context, deps Deps, uid int, live []string) []string {
	seen := make(map[string]bool, len(live))
	out := make([]string, 0, len(live))
	for _, p := range live {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if deps.Store == nil {
		return out
	}
	cached, err := deps.Store.ListGrantedFolders(ctx, uid)
	if err != nil {
		return out
	}
	for _, g := range cached {
		if g.Path == "" || seen[g.Path] {
			continue
		}
		seen[g.Path] = true
		out = append(out, g.Path)
	}
	return out
}

// readFSPath validates a user-supplied directory: absolute, and no literal
// ".." segment before cleaning (cleaning would silently swallow it).
func readFSPath(w http.ResponseWriter, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "bad_path", "缺少路径")
		return "", false
	}
	for _, seg := range strings.Split(filepath.ToSlash(trimmed), "/") {
		if seg == ".." {
			writeError(w, http.StatusBadRequest, "bad_path", "路径不能包含 ..")
			return "", false
		}
	}
	clean := filepath.Clean(trimmed)
	if !filepath.IsAbs(clean) {
		writeError(w, http.StatusBadRequest, "bad_path", "路径必须是绝对路径")
		return "", false
	}
	return clean, true
}

func readNeed(w http.ResponseWriter, r *http.Request) (string, bool) {
	need := strings.TrimSpace(r.URL.Query().Get("need"))
	if need == "" {
		return "read", true
	}
	if need != "read" && need != "write" {
		writeError(w, http.StatusBadRequest, "bad_need", "need 只能是 read 或 write")
		return "", false
	}
	return need, true
}

func aclSatisfies(res fnos.ACLResult, need string) bool {
	if need == "write" {
		return res.Writable
	}
	return res.Readable
}

func requestLang(r *http.Request) string {
	if lang := r.URL.Query().Get("lang"); lang != "" {
		return lang
	}
	return "zh-CN"
}

func writeFSError(w http.ResponseWriter, err error) {
	if os.IsPermission(err) {
		writeError(w, http.StatusForbidden, "forbidden", "没有访问该目录的权限")
		return
	}
	writeError(w, http.StatusBadGateway, "fnos_error", probe.Redact(err.Error()))
}
