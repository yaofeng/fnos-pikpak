package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putJSON(t *testing.T, h http.Handler, path, uid, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	if uid != "" {
		req.Header.Set("X-Trim-Userid", uid)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestSettingsDefaults(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	rec, body := getJSON(t, h, "/api/settings", "1000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	want := map[string]string{
		"download_concurrency": "2",
		"conflict":             "rename",
		"play_mode":            "proxy",
		"language":             "zh-CN",
	}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("%s = %v, want %v (body %v)", k, body[k], v, body)
		}
	}
}

func TestSettingsUpdateAndValidate(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	rec, body := putJSON(t, h, "/api/settings", "1000", `{"download_concurrency":"4","language":"en"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%v", rec.Code, body)
	}
	_, got := getJSON(t, h, "/api/settings", "1000")
	if got["download_concurrency"] != "4" || got["language"] != "en" {
		t.Fatalf("settings = %v", got)
	}
	// Untouched keys keep their defaults.
	if got["conflict"] != "rename" {
		t.Fatalf("conflict = %v", got["conflict"])
	}

	rec, body = putJSON(t, h, "/api/settings", "1000", `{"download_concurrency":"9"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "invalid_setting" {
		t.Fatalf("out of range: status=%d body=%v", rec.Code, body)
	}
	rec, body = putJSON(t, h, "/api/settings", "1000", `{"nope":"1"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "unknown_setting" {
		t.Fatalf("unknown key: status=%d body=%v", rec.Code, body)
	}
	rec, body = putJSON(t, h, "/api/settings", "1000", `{"conflict":"merge"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "invalid_setting" {
		t.Fatalf("bad enum: status=%d body=%v", rec.Code, body)
	}
}

func TestUserSettingsAreIsolated(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	if rec, body := putJSON(t, h, "/api/user-settings", "1000", `{"language":"en"}`); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %v", rec.Code, body)
	}
	_, a := getJSON(t, h, "/api/user-settings", "1000")
	_, b := getJSON(t, h, "/api/user-settings", "1001")
	if a["language"] != "en" {
		t.Fatalf("uid 1000 = %v", a)
	}
	if b["language"] != "zh-CN" {
		t.Fatalf("uid 1001 leaked another user's setting: %v", b)
	}
}

func TestSettingsKeepTheSearchKeyOutOfResponses(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	rec, body := putJSON(t, h, "/api/settings", "1000", `{"search_api_key":"sk-abc123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d body=%v", rec.Code, body)
	}
	if _, echoed := body["search_api_key"]; echoed {
		t.Fatalf("the key came back in the response: %v", body)
	}
	if body["search_api_key_set"] != "1" {
		t.Fatalf("search_api_key_set = %v", body["search_api_key_set"])
	}

	_, got := getJSON(t, h, "/api/settings", "1000")
	if _, echoed := got["search_api_key"]; echoed {
		t.Fatalf("a read returned the key: %v", got)
	}

	rec, body = putJSON(t, h, "/api/settings", "1000", `{"search_api_key":""}`)
	if rec.Code != http.StatusOK || body["search_api_key_set"] != "0" {
		t.Fatalf("clear status = %d body=%v", rec.Code, body)
	}

	rec, body = putJSON(t, h, "/api/settings", "1000", `{"search_api_key":"has space"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "invalid_setting" {
		t.Fatalf("bad key: status=%d body=%v", rec.Code, body)
	}
}

func TestSearchKeyIsNotAPerUserSetting(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	rec, body := putJSON(t, h, "/api/user-settings", "1000", `{"search_api_key":"sk-abc123"}`)
	if rec.Code != http.StatusBadRequest || errCode(body) != "not_user_setting" {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	_, got := getJSON(t, h, "/api/user-settings", "1000")
	if _, ok := got["search_api_key_set"]; ok {
		t.Fatalf("a global secret showed up in a user response: %v", got)
	}
}

func TestSettingsRejectNonJSON(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	h := NewMux(deps)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`download_concurrency=4`))
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func errCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestSettingsValidateDownloadDir(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	// The real guard asks fnOS for the user's folders; this test is about the
	// path shape and the storage, so every absolute path is allowed.
	deps.DirGuard = allowAll
	h := NewMux(deps)

	rec, _ := putJSON(t, h, "/api/user-settings", "1000", `{"download_dir":"relative/dir"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relative dir status = %d", rec.Code)
	}

	dir := t.TempDir()
	rec, body := putJSON(t, h, "/api/user-settings", "1000", `{"download_dir":"`+dir+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
	if body["download_dir"] != dir {
		t.Fatalf("download_dir = %v", body["download_dir"])
	}

	// An empty value clears it again.
	rec, body = putJSON(t, h, "/api/user-settings", "1000", `{"download_dir":""}`)
	if rec.Code != http.StatusOK || body["download_dir"] != "" {
		t.Fatalf("clear status = %d body=%v", rec.Code, body)
	}
}

func TestSettingsRejectDownloadDirOutsideGrants(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	deps.DirGuard = func(context.Context, int, string, bool) error { return ErrDirForbidden }
	h := NewMux(deps)

	rec, body := putJSON(t, h, "/api/user-settings", "1000", `{"download_dir":"/srv/other"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%v", rec.Code, body)
	}
}
