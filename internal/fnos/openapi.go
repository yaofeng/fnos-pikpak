package fnos

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultAPITimeout = 5 * time.Second

// Client talks to the fnOS Open API over its Unix socket. The bearer token is
// read from TRIM_API_TOKEN on every call because fnOS may rotate it.
type Client struct {
	SocketPath string
	AppName    string
	Token      func() string
	Timeout    time.Duration
}

type envelope struct {
	ReqID string          `json:"reqId"`
	Code  int             `json:"code"`
	Msg   string          `json:"msg"`
	Data  json.RawMessage `json:"data"`
}

func (c *Client) token() string {
	if c.Token != nil {
		return c.Token()
	}
	return os.Getenv("TRIM_API_TOKEN")
}

func (c *Client) httpClient() *http.Client {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = defaultAPITimeout
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", c.SocketPath)
			},
		},
	}
}

// Call invokes one Open API capability and decodes its data into out.
func (c *Client) Call(ctx context.Context, req string, data, out any) error {
	token := c.token()
	if token == "" {
		return fmt.Errorf("fnos: TRIM_API_TOKEN is empty")
	}

	reqID, err := randomID()
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"reqId":   reqID,
		"req":     req,
		"appName": c.AppName,
		"data":    data,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://localhost/api/v1/trimapp", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return fmt.Errorf("fnos: %s: %w", req, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("fnos: %s: read: %w", req, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fnos: %s: http %d: %s", req, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("fnos: %s: decode: %w", req, err)
	}
	if env.Code != 0 {
		return fmt.Errorf("fnos: %s: code %d: %s", req, env.Code, env.Msg)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("fnos: %s: data: %w", req, err)
		}
	}
	return nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

type PlatformConfig struct {
	Language string `json:"language"`
	Version  string `json:"version"`
}

// IsForbidden reports whether the gateway refused a capability with HTTP 403.
// fnOS answers that way for Open API scopes it does not grant to third-party
// apps, which is a deployment limit rather than a malformed request.
func IsForbidden(err error) bool {
	return err != nil && strings.Contains(err.Error(), ": http 403:")
}

func (c *Client) GetPlatformConfig(ctx context.Context) (PlatformConfig, error) {
	var out PlatformConfig
	err := c.Call(ctx, "trim.system.getPlatformConfig", map[string]any{}, &out)
	return out, err
}

// GetUserAccessibleFolders returns directory grants for one fnOS user. File
// grants are not included.
func (c *Client) GetUserAccessibleFolders(ctx context.Context, uid int) ([]string, error) {
	var out struct {
		Paths []string `json:"paths"`
	}
	err := c.Call(ctx, "trim.file.getUserAccessibleFolders", map[string]any{"uid": uid}, &out)
	return out.Paths, err
}

type ACLResult struct {
	Path      string `json:"path"`
	Readable  bool   `json:"readable"`
	Writable  bool   `json:"writable"`
	Deletable bool   `json:"deletable"`
}

func (c *Client) CheckUserACL(ctx context.Context, uid int, paths []string) ([]ACLResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("fnos: checkUserACL needs at least one path")
	}
	var out []ACLResult
	err := c.Call(ctx, "trim.file.checkUserACL", map[string]any{"uid": uid, "path": paths}, &out)
	return out, err
}

type ConvertedPath struct {
	Path         string `json:"path"`
	SemanticPath string `json:"semanticPath"`
}

// ConvertPath maps storage paths to the names fnOS shows users. Different
// fnOS builds answer with either the list itself as data or a
// {status, result} wrapper, so accept both, and surface the raw shape when
// neither matches (the M0 probe reports it to the page).
func (c *Client) ConvertPath(ctx context.Context, paths []string, lang string) ([]ConvertedPath, error) {
	var raw json.RawMessage
	err := c.Call(ctx, "trim.file.convertPath", map[string]any{"path": paths, "language": lang}, &raw)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var out []ConvertedPath
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("fnos: convertPath: %w", err)
		}
		if allFieldsUnrecognized(out) {
			return nil, fmt.Errorf("fnos: convertPath: unrecognized shape: %s", truncateRaw(trimmed))
		}
		return out, nil
	}

	var wrapped struct {
		Status int             `json:"status"`
		Result []ConvertedPath `json:"result"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err != nil {
		return nil, fmt.Errorf("fnos: convertPath: %w", err)
	}
	if wrapped.Status != 0 {
		return nil, fmt.Errorf("fnos: convertPath status %d", wrapped.Status)
	}
	return wrapped.Result, nil
}

// allFieldsUnrecognized reports whether every entry decoded to zero values,
// which means the payload used field names this build does not know.
func allFieldsUnrecognized(paths []ConvertedPath) bool {
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if p.Path != "" || p.SemanticPath != "" {
			return false
		}
	}
	return true
}

func truncateRaw(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
