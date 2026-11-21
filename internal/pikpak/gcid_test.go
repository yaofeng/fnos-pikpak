package pikpak

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { return len(p), nil }

func TestGCIDVectors(t *testing.T) {
	cases := []struct {
		name  string
		size  int64
		zeros bool
		data  string
		want  string
	}{
		{name: "empty", size: 0, want: "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
		{name: "abc", size: 3, data: "abc", want: "0d3ced9bec10a777aec23ccc353a8c08a633045e"},
		{name: "256KiB-zeros", size: 256 * 1024, zeros: true, want: "706fd5159f317e97543268985d8945dbd223206c"},
		{name: "256KiB+1-zeros", size: 256*1024 + 1, zeros: true, want: "d00cafa958f037c1093d8f45c7675caf6eacacc1"},
		{name: "200MiB-zeros", size: 200 << 20, zeros: true, want: "f509949e535970831951d44769e16c672d0f17f2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r io.Reader
			switch {
			case tc.zeros:
				r = io.LimitReader(zeroReader{}, tc.size)
			case tc.data != "":
				r = bytes.NewReader([]byte(tc.data))
			default:
				r = bytes.NewReader(nil)
			}
			got, err := GCID(r, tc.size)
			if err != nil {
				t.Fatalf("GCID: %v", err)
			}
			if got != tc.want {
				t.Fatalf("GCID = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestGCIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.txt")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := GCIDFile(path)
	if err != nil {
		t.Fatalf("GCIDFile: %v", err)
	}
	if got != "0d3ced9bec10a777aec23ccc353a8c08a633045e" {
		t.Fatalf("GCIDFile = %s", got)
	}
}

func TestGCIDRejectsShortReader(t *testing.T) {
	if _, err := GCID(bytes.NewReader([]byte("ab")), 3); err == nil {
		t.Fatal("expected error for truncated input")
	}
}

func TestBlockSize(t *testing.T) {
	cases := []struct {
		size int64
		want int64
	}{
		{0, 0x40000},
		{1, 0x40000},
		{512 * 0x40000, 0x40000}, // exactly 512 blocks
		{513 * 0x40000, 0x80000}, // 513 > 512 doubles
		{200 << 20, 0x80000},     // 800 -> 400 blocks
		{1 << 30, 0x200000},      // capped at 2 MiB
		{1 << 40, 0x200000},      // still capped
	}
	for _, tc := range cases {
		if got := blockSize(tc.size); got != tc.want {
			t.Fatalf("blockSize(%d) = %d, want %d", tc.size, got, tc.want)
		}
	}
}
