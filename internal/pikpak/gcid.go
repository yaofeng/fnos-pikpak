package pikpak

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// blockSize returns PikPak's GCID block size: it starts at 256 KiB and doubles
// while there would be more than 512 blocks, capped at 2 MiB.
func blockSize(size int64) int64 {
	p := int64(0x40000)
	for size/p > 0x200 && p < 0x200000 {
		p <<= 1
	}
	return p
}

// GCID computes PikPak's content hash for exactly size bytes from r: SHA-1 of
// each block, concatenated, then SHA-1 of that. Lower-case hex; callers
// upper-case it for the API. Empty input hashes the empty digest list, which
// is SHA-1("").
func GCID(r io.Reader, size int64) (string, error) {
	if size < 0 {
		return "", fmt.Errorf("pikpak: negative size %d", size)
	}
	p := blockSize(size)
	block := make([]byte, p)

	digests := sha1.New()
	remaining := size
	for remaining > 0 {
		n := remaining
		if n > p {
			n = p
		}
		if _, err := io.ReadFull(r, block[:n]); err != nil {
			return "", fmt.Errorf("pikpak: read block: %w", err)
		}
		sum := sha1.Sum(block[:n])
		digests.Write(sum[:])
		remaining -= n
	}

	return hex.EncodeToString(digests.Sum(nil)), nil
}

// GCIDFile hashes a file without loading it into memory.
func GCIDFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	return GCID(f, info.Size())
}
