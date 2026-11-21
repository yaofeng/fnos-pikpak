package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/fnostest"
)

func newFnosTestClient(t *testing.T) *fnos.Client {
	t.Helper()
	socket := fnostest.StartGateway(t, func(req string, data map[string]any) (int, string) {
		switch req {
		case "trim.file.getUserAccessibleFolders":
			return 0, `{"paths":["/vol1/home/me"]}`
		case "trim.file.checkUserACL":
			return 0, `[{"path":"/vol1/home/me","readable":true,"writable":true,"deletable":true}]`
		case "trim.file.convertPath":
			return 0, `{"status":0,"result":[{"path":"/vol1/home/me","semanticPath":"存储空间1/me"}]}`
		case "trim.system.getPlatformConfig":
			return 0, `{"language":"zh-CN","version":"1.2.0401"}`
		default:
			return 0, `{}`
		}
	})
	return &fnos.Client{
		SocketPath: socket,
		AppName:    "pikpak",
		Token:      func() string { return "test-token" },
	}
}

func TestProbeACL(t *testing.T) {
	h := NewMux(Deps{Version: "test", Fnos: newFnosTestClient(t)})

	body := strings.NewReader(`{"paths":["/vol1/home/me"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/fs/probe-acl", body)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}

	var out struct {
		Folders []string `json:"folders"`
		ACL     []struct {
			Path     string `json:"path"`
			Writable bool   `json:"writable"`
		} `json:"acl"`
		Semantic []struct {
			SemanticPath string `json:"semanticPath"`
		} `json:"semantic"`
		Under       map[string]bool   `json:"under"`
		PathErrors  map[string]string `json:"path_errors"`
		WritableOK  bool              `json:"writable_ok"`
		WritableErr string            `json:"writable_error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if len(out.Folders) != 1 || out.Folders[0] != "/vol1/home/me" {
		t.Fatalf("folders = %+v", out.Folders)
	}
	if len(out.ACL) != 1 || !out.ACL[0].Writable {
		t.Fatalf("acl = %+v", out.ACL)
	}
	if len(out.Semantic) != 1 || out.Semantic[0].SemanticPath != "存储空间1/me" {
		t.Fatalf("semantic = %+v", out.Semantic)
	}
	// The map must contain exactly the requested path.
	if len(out.Under) != 1 || !out.Under["/vol1/home/me"] {
		t.Fatalf("under = %+v", out.Under)
	}
	if !out.WritableOK || out.WritableErr != "" {
		t.Fatalf("writable_ok=%v err=%q", out.WritableOK, out.WritableErr)
	}
}

func TestProbeACLRejectsEmptyPaths(t *testing.T) {
	h := NewMux(Deps{Version: "test", Fnos: newFnosTestClient(t)})

	req := httptest.NewRequest(http.MethodPost, "/api/fs/probe-acl", strings.NewReader(`{"paths":[]}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}
