package fnos

import (
	"context"
	"strings"
	"testing"
	"time"

	"fnos-pikpak/internal/fnostest"
)

func newTestClient(t *testing.T, socket string) *Client {
	t.Helper()
	return &Client{
		SocketPath: socket,
		AppName:    "pikpak",
		Token:      func() string { return "test-token" },
	}
}

func TestCallSendsEnvelope(t *testing.T) {
	var got map[string]any
	socket := fnostest.StartGateway(t, func(_ string, data map[string]any) (int, string) {
		got = data
		return 0, `{"paths":["/vol1/home/me"]}`
	})

	c := newTestClient(t, socket)
	var out struct {
		Paths []string `json:"paths"`
	}
	if err := c.Call(context.Background(), "trim.file.getUserAccessibleFolders", map[string]any{"uid": 1000}, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if got["uid"].(float64) != 1000 {
		t.Fatalf("data = %+v", got)
	}
	if len(out.Paths) != 1 || out.Paths[0] != "/vol1/home/me" {
		t.Fatalf("paths = %+v", out.Paths)
	}
}

func TestCallReadsTokenEveryTime(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) { return 0, `{}` })
	c := newTestClient(t, socket)

	if err := c.Call(context.Background(), "trim.system.getPlatformConfig", nil, nil); err != nil {
		t.Fatalf("first call: %v", err)
	}

	c.Token = func() string { return "wrong-token" }
	if err := c.Call(context.Background(), "trim.system.getPlatformConfig", nil, nil); err == nil {
		t.Fatal("call with a rotated token succeeded, want 401")
	}
}

func TestCallSurfacesGatewayError(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) { return 7, `{}` })
	c := newTestClient(t, socket)

	err := c.Call(context.Background(), "trim.file.checkUserACL", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "code 7") {
		t.Fatalf("err = %v", err)
	}
}

func TestCallTimesOut(t *testing.T) {
	socket := fnostest.StartSlowGateway(t)
	c := newTestClient(t, socket)
	c.Timeout = 200 * time.Millisecond

	start := time.Now()
	err := c.Call(context.Background(), "trim.system.getPlatformConfig", nil, nil)
	if err == nil {
		t.Fatal("want timeout error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestCheckUserACLParsesArray(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) {
		return 0, `[{"path":"/vol1/a","readable":true,"writable":false,"deletable":false}]`
	})
	c := newTestClient(t, socket)

	results, err := c.CheckUserACL(context.Background(), 1000, []string{"/vol1/a"})
	if err != nil {
		t.Fatalf("acl: %v", err)
	}
	if len(results) != 1 || !results[0].Readable || results[0].Writable {
		t.Fatalf("results = %+v", results)
	}
}

func TestConvertPathParsesArrayResponse(t *testing.T) {
	socket := fnostest.StartGateway(t, func(req string, data map[string]any) (int, string) {
		if req != "trim.file.convertPath" {
			t.Errorf("req = %q", req)
		}
		// The real gateway returns the result list directly as data, without
		// the {status, result} wrapper the M0 probe assumed.
		return 0, `[{"path":"/vol1/1001/private/pikpak","semanticPath":"/我的文件/pikpak"}]`
	})
	c := newTestClient(t, socket)

	got, err := c.ConvertPath(context.Background(), []string{"/vol1/1001/private/pikpak"}, "zh-CN")
	if err != nil {
		t.Fatalf("convertPath: %v", err)
	}
	if len(got) != 1 || got[0].SemanticPath != "/我的文件/pikpak" {
		t.Fatalf("got = %+v", got)
	}
}

func TestConvertPathParsesWrappedResponse(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) {
		return 0, `{"status":0,"result":[{"path":"/vol1/a","semanticPath":"/文件/a"}]}`
	})
	c := newTestClient(t, socket)

	got, err := c.ConvertPath(context.Background(), []string{"/vol1/a"}, "zh-CN")
	if err != nil {
		t.Fatalf("convertPath: %v", err)
	}
	if len(got) != 1 || got[0].Path != "/vol1/a" {
		t.Fatalf("got = %+v", got)
	}
}

func TestConvertPathReportsUnrecognizedShape(t *testing.T) {
	socket := fnostest.StartGateway(t, func(string, map[string]any) (int, string) {
		return 0, `[{"vol":"/vol1/a","show":"/文件/a"}]`
	})
	c := newTestClient(t, socket)

	_, err := c.ConvertPath(context.Background(), []string{"/vol1/a"}, "zh-CN")
	if err == nil || !strings.Contains(err.Error(), `"vol"`) {
		t.Fatalf("err = %v, want the raw shape in the message", err)
	}
}

func TestMissingTokenFailsWithoutRequest(t *testing.T) {
	c := &Client{SocketPath: "/nonexistent.sock", AppName: "pikpak", Token: func() string { return "" }}
	err := c.Call(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "TRIM_API_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}
