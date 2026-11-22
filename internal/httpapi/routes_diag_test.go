package httpapi

import (
	"net/http"
	"testing"

	"fnos-pikpak/internal/store"
)

func TestDiagSummarisesJobsAndStorage(t *testing.T) {
	deps, st := newAuthTestDeps(t, "http://127.0.0.1:1")
	bindTestUser(t, st, 1000)
	ctx := t.Context()

	if err := st.PutDownloadJob(ctx, store.DownloadJob{
		ID: "d1", UID: 1000, FileID: "f1", FileName: "a", DestDir: "/tmp", DestPath: "/tmp/a",
		Status: "failed", ErrorCode: "download_failed", ErrorMessage: "boom",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutDownloadJob(ctx, store.DownloadJob{
		ID: "d2", UID: 1000, FileID: "f2", FileName: "b", DestDir: "/tmp", DestPath: "/tmp/b", Status: "done",
	}); err != nil {
		t.Fatal(err)
	}

	h := NewMux(deps)
	rec, body := getJSON(t, h, "/api/diag", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["version"] != "test" || body["uid"] != float64(1000) || body["bound"] != true {
		t.Fatalf("body = %v", body)
	}
	if size, _ := body["db_bytes"].(float64); size <= 0 {
		t.Fatalf("db_bytes = %v", body["db_bytes"])
	}
	downloads, _ := body["downloads"].(map[string]any)
	if downloads["failed"] != float64(1) || downloads["done"] != float64(1) {
		t.Fatalf("downloads = %v", body["downloads"])
	}
	failures, _ := body["recent_failures"].([]any)
	if len(failures) != 1 {
		t.Fatalf("failures = %v", body["recent_failures"])
	}
	first, _ := failures[0].(map[string]any)
	if first["kind"] != "download" || first["error_code"] != "download_failed" {
		t.Fatalf("failure = %v", first)
	}
}

func TestDiagRequiresIdentity(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	rec, _ := getJSON(t, NewMux(deps), "/api/diag", "")
	if rec.Code == http.StatusOK {
		t.Fatalf("diag answered without identity")
	}
}
