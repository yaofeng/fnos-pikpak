package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fnos-pikpak/internal/probe"
)

type fakeProbe struct{}

func (fakeProbe) Name() string { return "fake" }
func (fakeProbe) Run(context.Context) probe.Result {
	return probe.Result{Name: "fake", Status: probe.Pass, Detail: "ok"}
}

func TestProbeRunStoresReport(t *testing.T) {
	reg := &probe.Registry{}
	reg.Add(fakeProbe{})
	h := NewMux(Deps{Version: "test", Probes: reg, Debug: true})

	run := httptest.NewRequest(http.MethodPost, "/api/probes/run", nil)
	run.Header.Set("X-Trim-Userid", "1000")
	run.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, run)

	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body)
	}
	var runBody struct {
		Results []probe.Result `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &runBody); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if len(runBody.Results) != 1 || runBody.Results[0].Status != probe.Pass {
		t.Fatalf("results = %+v", runBody.Results)
	}

	report := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	report.Header.Set("X-Trim-Userid", "1000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, report)

	if rec.Code != http.StatusOK {
		t.Fatalf("report status=%d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "pikpak-m0-report.json") {
		t.Fatalf("disposition = %q", cd)
	}
	var stored storedReport
	if err := json.Unmarshal(rec.Body.Bytes(), &stored); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if stored.UID != 1000 || stored.Version != "test" || len(stored.Results) != 1 {
		t.Fatalf("report = %+v", stored)
	}
}

func TestReportIsPerUser(t *testing.T) {
	reg := &probe.Registry{}
	reg.Add(fakeProbe{})
	h := NewMux(Deps{Version: "test", Probes: reg, Debug: true})

	run := httptest.NewRequest(http.MethodPost, "/api/probes/run", nil)
	run.Header.Set("X-Trim-Userid", "1000")
	run.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), run)

	other := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	other.Header.Set("X-Trim-Userid", "1001")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, other)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another user", rec.Code)
	}
}
