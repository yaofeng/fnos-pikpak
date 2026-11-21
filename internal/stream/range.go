package stream

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrRangeNotSatisfiable = errors.New("stream: range not satisfiable")
	ErrInvalidRange        = errors.New("stream: invalid range")
)

// ParseRange handles a single byte range. Multiple ranges are rejected: the
// M0 probe and the eventual player only need one.
func ParseRange(header string, size int64) (start, end int64, err error) {
	if header == "" {
		if size == 0 {
			return 0, -1, nil
		}
		return 0, size - 1, nil
	}
	if !strings.HasPrefix(header, "bytes=") || strings.Contains(header, ",") {
		return 0, 0, ErrInvalidRange
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, ErrInvalidRange
	}
	startPart, endPart := parts[0], parts[1]

	if startPart == "" {
		n, err := strconv.ParseInt(endPart, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, ErrInvalidRange
		}
		if n > size {
			n = size
		}
		if size == 0 {
			return 0, 0, ErrRangeNotSatisfiable
		}
		return size - n, size - 1, nil
	}

	start, err = strconv.ParseInt(startPart, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, ErrInvalidRange
	}
	if start >= size {
		return 0, 0, ErrRangeNotSatisfiable
	}
	if endPart == "" {
		return start, size - 1, nil
	}
	end, err = strconv.ParseInt(endPart, 10, 64)
	if err != nil || end < start {
		return 0, 0, ErrInvalidRange
	}
	if end >= size {
		end = size - 1
	}
	return start, end, nil
}

// ServeFile streams a single file range. It never buffers the whole file.
func ServeFile(w http.ResponseWriter, r *http.Request, path string, size int64) {
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")

	start, end, err := ParseRange(r.Header.Get("Range"), size)
	switch {
	case errors.Is(err, ErrRangeNotSatisfiable):
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case err != nil:
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open probe file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	status := http.StatusOK
	length := int64(0)
	if size > 0 {
		length = end - start + 1
	}
	if r.Header.Get("Range") != "" && size > 0 {
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)

	if r.Method == http.MethodHead || length == 0 {
		return
	}
	_, _ = io.CopyN(w, io.NewSectionReader(f, start, length), length)
}

// EnsureProbeFile creates a deterministic file of the requested size and
// reuses it when it already matches.
func EnsureProbeFile(dir string, size int64) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("stream: probe size must be positive")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "probe.bin")
	if st, err := os.Stat(path); err == nil && st.Size() == size {
		return path, nil
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	const block = 64 << 10
	buf := make([]byte, block)
	written := int64(0)
	for written < size {
		n := int64(block)
		if remaining := size - written; remaining < n {
			n = remaining
		}
		for i := range buf[:n] {
			buf[i] = byte((written + int64(i)) / block % 251)
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return "", err
		}
		written += n
	}
	return path, nil
}
