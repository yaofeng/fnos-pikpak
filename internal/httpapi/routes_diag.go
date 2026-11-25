package httpapi

import (
	"net/http"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/store"
)

// registerDiagRoutes exposes a read-only summary for the settings page. It is
// scoped to the calling user: job counts and failures never cross accounts.
func registerDiagRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /api/diag", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())
		ctx := r.Context()

		counts, err := deps.Store.JobCounts(ctx, id.UID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取任务统计")
			return
		}
		failures, err := deps.Store.RecentJobFailures(ctx, id.UID, 10)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "无法读取错误记录")
			return
		}
		dbBytes, err := deps.Store.DBSize()
		if err != nil {
			dbBytes = 0
		}

		bound := false
		if deps.Sessions != nil {
			if _, err := deps.Sessions.ClientForAccount(ctx, id.UID, ""); err == nil {
				bound = true
			}
		}
		folders, _ := deps.Store.ListGrantedFolders(ctx, id.UID)

		if failures == nil {
			failures = []store.JobFailure{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":         deps.Version,
			"uid":             id.UID,
			"bound":           bound,
			"db_bytes":        dbBytes,
			"downloads":       counts.Downloads,
			"uploads":         counts.Uploads,
			"recent_failures": failures,
			"granted_folders": len(folders),
			"fnos_available":  deps.Fnos != nil,
		})
	})
}
