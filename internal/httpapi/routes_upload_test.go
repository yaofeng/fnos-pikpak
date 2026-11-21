package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/store"
)

func fakeOSSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == http.MethodPost && q.Has("uploads"):
			_, _ = io.WriteString(w, `<?xml version="1.0"?><InitiateMultipartUploadResult><Key>k</Key><UploadId>up-1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut && q.Get("partNumber") != "":
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"e"`)
		case r.Method == http.MethodPost && q.Get("uploadId") != "":
			_, _ = io.WriteString(w, `<ok`)
		case r.Method == http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
		default:
			t.Errorf("unexpected OSS request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
}

func fakeUploadDrive(t *testing.T, ossURL string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/drive/v1/files":
			_, _ = io.WriteString(w, `{
				"file": {"id": "f1", "name": "pikpak-probe-20m.bin", "size": "20971520"},
				"resumable": {"provider": "PROVIDER_ALIYUN", "params": {
					"access_key_id": "id", "access_key_secret": "secret", "bucket": "bkt",
					"endpoint": "`+ossURL+`", "key": "k", "security_token": "sts"
				}}
			}`)
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v1/files/f1":
			_, _ = io.WriteString(w, `{"id":"f1","name":"pikpak-probe-20m.bin","phase":"PHASE_TYPE_COMPLETE","size":"20971520"}`)
		default:
			t.Errorf("unexpected drive request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestUploadProbeConfigAndGeneratedFile(t *testing.T) {
	t.Setenv("TRIM_PKGTMP", t.TempDir())
	oss := fakeOSSServer(t)
	defer oss.Close()
	drive := fakeUploadDrive(t, oss.URL)
	defer drive.Close()

	deps, st := newAuthTestDeps(t, drive.URL)
	if err := st.PutCredential(t.Context(), store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at", RefreshToken: "rt",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}
	h := NewMux(deps)

	rec, cfg := getJSON(t, h, "/api/upload/probe/config", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", rec.Code, rec.Body)
	}
	genPath, _ := cfg["generated_path"].(string)
	if genPath == "" || !strings.HasSuffix(genPath, "pikpak-probe-20m.bin") {
		t.Fatalf("generated_path = %q", genPath)
	}
	if cfg["generated_size"] != float64(20<<20) {
		t.Fatalf("generated_size = %v", cfg["generated_size"])
	}

	body, _ := json.Marshal(map[string]string{"local_path": genPath})
	req := httptest.NewRequest(http.MethodPost, "/api/upload/probe", strings.NewReader(string(body)))
	req.Header.Set("X-Trim-Userid", "1000")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe status=%d body=%s", rec.Code, rec.Body)
	}

	var out struct {
		Path      string   `json:"local_path"`
		Size      int64    `json:"size"`
		GCID      string   `json:"gcid"`
		Instant   bool     `json:"instant"`
		Provider  string   `json:"provider"`
		ParamKeys []string `json:"param_keys"`
		File      struct {
			ID    string `json:"id"`
			Phase string `json:"phase"`
		} `json:"file"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body)
	}
	if out.Size != 20<<20 || out.Instant || out.Provider != "PROVIDER_ALIYUN" {
		t.Fatalf("out = %+v", out)
	}
	if len(out.ParamKeys) != 6 {
		t.Fatalf("param_keys = %v", out.ParamKeys)
	}
	for _, k := range out.ParamKeys {
		if k == "sts" || k == "secret" {
			t.Fatalf("param keys leaked a value: %v", out.ParamKeys)
		}
	}
	if out.File.ID != "f1" || out.File.Phase != "PHASE_TYPE_COMPLETE" {
		t.Fatalf("file = %+v", out.File)
	}

	// The file is deterministic: a second run produces the same GCID.
	want, err := pikpak.GCIDFile(genPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.GCID != want {
		t.Fatalf("gcid = %s, want %s", out.GCID, want)
	}
}

func TestUploadProbeRejectsPathOutsideACL(t *testing.T) {
	t.Setenv("TRIM_PKGTMP", t.TempDir())
	outside := filepath.Join(t.TempDir(), "private.bin")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.Fnos = newFnosTestClient(t)
	h := NewMux(deps)

	body, _ := json.Marshal(map[string]string{"local_path": outside})
	req := httptest.NewRequest(http.MethodPost, "/api/upload/probe", strings.NewReader(string(body)))
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestUploadProbeRequiresBinding(t *testing.T) {
	t.Setenv("TRIM_PKGTMP", t.TempDir())
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	_, cfg := getJSON(t, h, "/api/upload/probe/config", "1000")
	genPath, _ := cfg["generated_path"].(string)

	body, _ := json.Marshal(map[string]string{"local_path": genPath})
	req := httptest.NewRequest(http.MethodPost, "/api/upload/probe", strings.NewReader(string(body)))
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestUploadProbeValidatesBody(t *testing.T) {
	t.Setenv("TRIM_PKGTMP", t.TempDir())
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	for _, body := range []string{`{}`, `{"local_path":"relative/x.bin"}`, `not json`} {
		req := httptest.NewRequest(http.MethodPost, "/api/upload/probe", strings.NewReader(body))
		req.Header.Set("X-Trim-Userid", "1000")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status=%d, want 400", body, rec.Code)
		}
	}
}
