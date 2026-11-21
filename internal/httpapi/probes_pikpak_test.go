package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fnos-pikpak/internal/pikpak"
	"fnos-pikpak/internal/probe"
	"fnos-pikpak/internal/store"
)

func TestPikPakProbesSkipWhenUnbound(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)

	results := reg.Run(probe.WithUID(context.Background(), 1000))
	if len(results) != 5 {
		t.Fatalf("results = %d, want 5", len(results))
	}
	for _, r := range results {
		if r.Status != probe.Skip {
			t.Fatalf("%s status = %s (%s), want skip", r.Name, r.Status, r.Detail)
		}
	}
}

func TestPikPakProbesSkipWithoutUID(t *testing.T) {
	deps, _ := newAuthTestDeps(t, "http://127.0.0.1:1")
	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)

	results := reg.Run(context.Background())
	for _, r := range results {
		if r.Status != probe.Skip {
			t.Fatalf("%s status = %s, want skip", r.Name, r.Status)
		}
	}
}

func TestPikPakProbesRunWhenBound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v1/about":
			_, _ = io.WriteString(w, `{"quota":{"limit":"1000","usage":"250"},"expires_at":""}`)
		case "/drive/v1/privilege/vip":
			_, _ = io.WriteString(w, `{"data":{"type":"platinum","expire":"2027-01-01T00:00:00Z"}}`)
		case "/drive/v1/search":
			_, _ = io.WriteString(w, `{"files":[{"id":"s1","name":"hit","size":"1"}]}`)
		case "/drive/v1/files:search":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not_found"}`)
		case "/drive/v1/files":
			_, _ = io.WriteString(w, `{"files":[{"id":"f1","name":"movie.mkv","kind":"drive#file","size":"42"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	deps, st := newAuthTestDeps(t, srv.URL)
	ctx := context.Background()
	if err := st.PutCredential(ctx, store.Credential{
		UID: 1000, AuthType: "oauth", ClientID: pikpak.DefaultClientID,
		AccessToken: "at", RefreshToken: "rt",
		DeviceID:        "0123456789abcdef0123456789abcdef",
		AccessExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutCredential: %v", err)
	}

	reg := &probe.Registry{}
	RegisterPikPakProbes(reg, deps.Sessions)
	results := reg.Run(probe.WithUID(ctx, 1000))

	byName := map[string]probe.Result{}
	for _, r := range results {
		byName[r.Name] = r
	}
	for name, want := range map[string]probe.Status{
		"pikpak.list": probe.Pass, "pikpak.quota": probe.Pass,
		"pikpak.vip": probe.Pass, "pikpak.order": probe.Pass, "pikpak.search": probe.Pass,
	} {
		r, ok := byName[name]
		if !ok {
			t.Fatalf("probe %s missing", name)
		}
		if r.Status != want {
			t.Fatalf("%s status = %s (%s), want %s", name, r.Status, r.Detail, want)
		}
	}

	// Quota numbers must survive the string-typed JSON.
	var quota struct {
		Limit int64 `json:"quota_limit"`
		Usage int64 `json:"quota_usage"`
	}
	raw, _ := json.Marshal(byName["pikpak.quota"].Data)
	_ = json.Unmarshal(raw, &quota)
	if quota.Limit != 1000 || quota.Usage != 250 {
		t.Fatalf("quota data = %s", raw)
	}

	// List data carries the first page for the report.
	if byName["pikpak.list"].Data["items"] == nil {
		t.Fatalf("list data = %v", byName["pikpak.list"].Data)
	}
}
