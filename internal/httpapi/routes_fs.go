package httpapi

import (
	"net/http"

	"fnos-pikpak/internal/fnos"
)

func registerFSRoutes(mux *http.ServeMux, deps Deps) {
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
			writeError(w, http.StatusBadGateway, "fnos_error", err.Error())
			return
		}
		acl, err := deps.Fnos.CheckUserACL(ctx, id.UID, body.Paths)
		if err != nil {
			writeError(w, http.StatusBadGateway, "fnos_error", err.Error())
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
