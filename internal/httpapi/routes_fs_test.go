package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fnos-pikpak/internal/fnos"
	"fnos-pikpak/internal/fnostest"
	"fnos-pikpak/internal/store"
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
	h := NewMux(Deps{Version: "test", Fnos: newFnosTestClient(t), Debug: true})

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
	h := NewMux(Deps{Version: "test", Fnos: newFnosTestClient(t), Debug: true})

	req := httptest.NewRequest(http.MethodPost, "/api/fs/probe-acl", strings.NewReader(`{"paths":[]}`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

type fsEnv struct {
	deps Deps
	st   *store.Store
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// newFSEnv builds Deps whose fnOS Open API answers with the given granted
// roots. writable decides checkUserACL's writable flag per path; nil means
// every path is writable.
func newFSEnv(t *testing.T, roots []string, writable func(string) bool) fsEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pikpak.db"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	socket := fnostest.StartGateway(t, func(req string, data map[string]any) (int, string) {
		switch req {
		case "trim.file.getUserAccessibleFolders":
			return 0, mustJSON(t, map[string]any{"paths": roots})
		case "trim.file.checkUserACL":
			paths, _ := data["path"].([]any)
			out := make([]map[string]any, 0, len(paths))
			for _, raw := range paths {
				p, _ := raw.(string)
				// The real Open API only grants paths the user actually has
				// access to, so outside the granted roots nothing passes.
				under := fnos.IsUnderAny(p, roots)
				ok := under && (writable == nil || writable(p))
				out = append(out, map[string]any{
					"path": p, "readable": under, "writable": ok, "deletable": ok,
				})
			}
			return 0, mustJSON(t, out)
		case "trim.file.convertPath":
			paths, _ := data["path"].([]any)
			out := make([]map[string]any, 0, len(paths))
			for _, raw := range paths {
				p, _ := raw.(string)
				out = append(out, map[string]any{"path": p, "semanticPath": "语义" + p})
			}
			return 0, mustJSON(t, out)
		default:
			return 0, `{}`
		}
	})
	return fsEnv{
		deps: Deps{
			Version: "test", Store: st,
			Fnos: &fnos.Client{
				SocketPath: socket,
				AppName:    "pikpak",
				Token:      func() string { return "test-token" },
			},
		},
		st: st,
	}
}

func TestFSRoutesWithoutFnosReturn503(t *testing.T) {
	env := newFSEnv(t, nil, nil)
	env.deps.Fnos = nil
	h := NewMux(env.deps)

	for _, path := range []string{"/api/fs/folders", "/api/fs/list?path=/tmp", "/api/fs/check?path=/tmp&need=read"} {
		rec, body := getJSON(t, h, path, "1000")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d body=%v", path, rec.Code, body)
		}
		errObj, _ := body["error"].(map[string]any)
		if errObj["code"] != "fnos_unavailable" {
			t.Fatalf("%s: error = %v", path, body)
		}
	}
}

func TestFSFoldersReturnSemanticPaths(t *testing.T) {
	env := newFSEnv(t, []string{"/vol1/home/me"}, nil)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/folders", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body)
	}
	first, _ := items[0].(map[string]any)
	if first["path"] != "/vol1/home/me" || first["semantic_path"] != "语义/vol1/home/me" {
		t.Fatalf("item = %v", first)
	}
}

func TestFSListRejectsBadPaths(t *testing.T) {
	root := t.TempDir()
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	for _, path := range []string{
		"/api/fs/list?path=relative/dir",
		"/api/fs/list?path=" + root + "/../../etc",
		"/api/fs/list",
	} {
		rec, body := getJSON(t, h, path, "1000")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body=%v", path, rec.Code, body)
		}
	}
}

func TestFSListRejectsForeignPath(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/list?path="+other, "1000")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestFSListRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/list?path="+filepath.Join(root, "escape"), "1000")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestFSListOmitsEscapingEntries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/list?path="+root, "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %v", body)
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "sub" || first["is_dir"] != true {
		t.Fatalf("directories must come first: %v", items)
	}
	second, _ := items[1].(map[string]any)
	if second["name"] != "a.txt" || second["size"] != float64(5) {
		t.Fatalf("second = %v", second)
	}
}

func TestFSListMissingPathIs404(t *testing.T) {
	root := t.TempDir()
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/list?path="+filepath.Join(root, "nope"), "1000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestFSAuthorizeGrantsFolder(t *testing.T) {
	root := t.TempDir()
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/fs/authorize", "1000", `{"path":"`+root+`","need":"write"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	grants, err := env.st.ListGrantedFolders(t.Context(), 1000)
	if err != nil || len(grants) != 1 || grants[0].Path != root {
		t.Fatalf("grants = %+v err = %v", grants, err)
	}
}

func TestFSAuthorizeRejectsForeignPath(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	env := newFSEnv(t, []string{root}, nil)
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/fs/authorize", "1000", `{"path":"`+other+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if grants, _ := env.st.ListGrantedFolders(t.Context(), 1000); len(grants) != 0 {
		t.Fatalf("grants = %+v", grants)
	}
}

func TestFSAuthorizeRequiresWriteACLForWriteNeed(t *testing.T) {
	root := t.TempDir()
	env := newFSEnv(t, []string{root}, func(string) bool { return false })
	h := NewMux(env.deps)

	rec, body := postJSON(t, h, "/api/fs/authorize", "1000", `{"path":"`+root+`","need":"write"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}

func TestFSCheckReportsACL(t *testing.T) {
	root := t.TempDir()
	env := newFSEnv(t, []string{root}, func(p string) bool { return p != filepath.Join(root, "ro") })
	if err := os.Mkdir(filepath.Join(root, "ro"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewMux(env.deps)

	rec, body := getJSON(t, h, "/api/fs/check?path="+root+"&need=write", "1000")
	if rec.Code != http.StatusOK || body["ok"] != true || body["writable"] != true {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}

	rec, body = getJSON(t, h, "/api/fs/check?path="+filepath.Join(root, "ro")+"&need=write", "1000")
	if rec.Code != http.StatusOK || body["ok"] != false || body["writable"] != false {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}
