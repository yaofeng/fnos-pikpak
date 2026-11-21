package pikpak

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

var ErrUnsupportedProvider = errors.New("pikpak: unsupported upload provider")

const (
	// partSize is PikPak's own multipart default (6 MiB).
	partSize = int64(6 << 20)
	// multipartThreshold is where the community implementations switch from a
	// single PUT to multipart.
	multipartThreshold = int64(10 << 20)
	// partConcurrency matches the official CLI's default.
	partConcurrency = 3
)

var uploadPollInterval = 2 * time.Second

type UnsupportedProviderError struct {
	Provider  string
	ParamKeys []string
}

func (e *UnsupportedProviderError) Error() string {
	return fmt.Sprintf("pikpak: unsupported upload provider %s (params: %s)",
		e.Provider, strings.Join(e.ParamKeys, ", "))
}

func (e *UnsupportedProviderError) Unwrap() error { return ErrUnsupportedProvider }

type Resumable struct {
	Provider string
	Params   map[string]string
}

type UploadTicket struct {
	File      *File
	Resumable *Resumable
	// Size is the size that was requested. PikPak sometimes omits `file` from
	// the resumable response, and the upload still needs to know the size.
	Size int64
}

type UploadRequest struct {
	Name     string
	Size     int64
	GCID     string
	ParentID string
}

func (c *Client) RequestUpload(ctx context.Context, in UploadRequest) (UploadTicket, error) {
	body := map[string]any{
		"kind":        "drive#file",
		"name":        in.Name,
		"size":        in.Size,
		"hash":        strings.ToUpper(in.GCID),
		"upload_type": "UPLOAD_TYPE_RESUMABLE",
		"objProvider": map[string]string{"provider": "UPLOAD_TYPE_UNKNOWN"},
		"folder_type": "NORMAL",
	}
	if in.ParentID != "" {
		body["parent_id"] = in.ParentID
	}

	var raw struct {
		File      *File `json:"file"`
		Resumable *struct {
			Provider string            `json:"provider"`
			Params   map[string]string `json:"params"`
		} `json:"resumable"`
	}
	if err := c.do(ctx, http.MethodPost, c.BaseDrive+"/drive/v1/files", body, &raw); err != nil {
		return UploadTicket{}, err
	}

	ticket := UploadTicket{File: raw.File, Size: in.Size}
	if raw.Resumable != nil {
		ticket.Resumable = &Resumable{Provider: raw.Resumable.Provider, Params: raw.Resumable.Params}
	}
	if ticket.File == nil && ticket.Resumable == nil {
		return UploadTicket{}, errors.New("pikpak: upload response has neither file nor resumable session")
	}
	return ticket, nil
}

// CompleteUpload pushes the bytes to the provider. A nil Resumable means the
// file was deduplicated server-side and nothing is transferred.
func (c *Client) CompleteUpload(ctx context.Context, ticket UploadTicket, src io.Reader) error {
	if ticket.Resumable == nil {
		return nil
	}
	r := ticket.Resumable
	if r.Provider != "PROVIDER_ALIYUN" {
		return &UnsupportedProviderError{Provider: r.Provider, ParamKeys: sortedKeys(r.Params)}
	}

	params := r.Params
	endpoint, err := normalizeOSSEndpoint(params["endpoint"])
	if err != nil {
		return err
	}
	client, err := oss.New(endpoint, params["access_key_id"], params["access_key_secret"],
		oss.SecurityToken(params["security_token"]))
	if err != nil {
		return fmt.Errorf("pikpak: oss client: %w", err)
	}
	bucket, err := client.Bucket(params["bucket"])
	if err != nil {
		return fmt.Errorf("pikpak: oss bucket: %w", err)
	}

	size := ticket.Size
	if ticket.File != nil && ticket.File.Size > 0 {
		size = ticket.File.Size
	}
	key := params["key"]

	if size < multipartThreshold {
		if err := bucket.PutObject(key, src, oss.ContentLength(size)); err != nil {
			return fmt.Errorf("pikpak: oss put: %w", err)
		}
		return nil
	}
	return c.uploadMultipart(ctx, bucket, key, src)
}

func (c *Client) uploadMultipart(_ context.Context, bucket *oss.Bucket, key string, src io.Reader) error {
	imur, err := bucket.InitiateMultipartUpload(key)
	if err != nil {
		return fmt.Errorf("pikpak: oss initiate multipart: %w", err)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		parts    []oss.UploadPart
		firstErr error
	)
	sem := make(chan struct{}, partConcurrency)

	for partNumber := 1; ; partNumber++ {
		buf := make([]byte, partSize)
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			sem <- struct{}{}
			wg.Add(1)
			go func(pn int, data []byte) {
				defer wg.Done()
				defer func() { <-sem }()
				part, err := bucket.UploadPart(imur, bytes.NewReader(data), int64(len(data)), pn)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					return
				}
				parts = append(parts, part)
			}(partNumber, buf[:n])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				break
			}
			wg.Wait()
			return fmt.Errorf("pikpak: read upload source: %w", readErr)
		}
	}
	wg.Wait()
	if firstErr != nil {
		return fmt.Errorf("pikpak: oss upload part: %w", firstErr)
	}

	if _, err := bucket.CompleteMultipartUpload(imur, parts); err != nil {
		// PikPak's OSS responses can be malformed while the upload itself
		// succeeded; only a truncated/missing XML body is tolerated.
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "EOF") {
			return nil
		}
		return fmt.Errorf("pikpak: oss complete multipart: %w", err)
	}
	return nil
}

// normalizeOSSEndpoint applies OpenList's rule: some clients get a
// vip-lixian-* host that does not accept uploads; mypikpak.net does.
func normalizeOSSEndpoint(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("pikpak: oss endpoint missing")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("pikpak: oss endpoint %q: %w", raw, err)
	}
	host := u.Hostname()
	if strings.HasPrefix(host, "vip-lixian") {
		u.Host = "mypikpak.net"
	}
	return u.String(), nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *Client) WaitFileComplete(ctx context.Context, fileID string, timeout time.Duration) (File, error) {
	deadline := time.Now().Add(timeout)
	for {
		u := c.BaseDrive + "/drive/v1/files/" + url.PathEscape(fileID) + "?usage=FETCH"
		var f File
		if err := c.do(ctx, http.MethodGet, u, nil, &f); err != nil {
			return File{}, err
		}
		if f.Phase == "PHASE_TYPE_COMPLETE" {
			return f, nil
		}
		if time.Now().After(deadline) {
			return f, fmt.Errorf("pikpak: file %s still %s after %s", fileID, f.Phase, timeout)
		}
		select {
		case <-time.After(uploadPollInterval):
		case <-ctx.Done():
			return File{}, ctx.Err()
		}
	}
}
