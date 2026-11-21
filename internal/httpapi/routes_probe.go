package httpapi

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/probe"
)

type storedReport struct {
	UID         int            `json:"uid"`
	Version     string         `json:"version"`
	GeneratedAt time.Time      `json:"generated_at"`
	Summary     map[string]int `json:"summary"`
	Results     []probe.Result `json:"results"`
}

type reportStore struct {
	mu    sync.Mutex
	byUID map[int]storedReport
}

func newReportStore() *reportStore {
	return &reportStore{byUID: map[int]storedReport{}}
}

func summarize(results []probe.Result) map[string]int {
	s := map[string]int{"pass": 0, "fail": 0, "warn": 0, "skip": 0}
	for _, r := range results {
		s[string(r.Status)]++
	}
	return s
}

func registerProbeRoutes(mux *http.ServeMux, deps Deps, reports *reportStore) {
	mux.HandleFunc("GET /api/probes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"probes": deps.Probes.Names()})
	})

	mux.HandleFunc("POST /api/probes/run", func(w http.ResponseWriter, r *http.Request) {
		if !requireJSONBody(w, r) {
			return
		}
		id, _ := fnos.IdentityFrom(r.Context())
		ctx := probe.WithUID(r.Context(), id.UID)
		results := deps.Probes.Run(ctx)

		reports.mu.Lock()
		reports.byUID[id.UID] = storedReport{
			UID:         id.UID,
			Version:     deps.Version,
			GeneratedAt: time.Now(),
			Summary:     summarize(results),
			Results:     results,
		}
		reports.mu.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})

	mux.HandleFunc("GET /api/report", func(w http.ResponseWriter, r *http.Request) {
		id, _ := fnos.IdentityFrom(r.Context())

		reports.mu.Lock()
		report, ok := reports.byUID[id.UID]
		reports.mu.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "no_report", "还没有运行过探针")
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="pikpak-m0-report.json"`)
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	})
}
