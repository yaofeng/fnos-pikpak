package fnos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsUnderAny(t *testing.T) {
	cases := []struct {
		path  string
		roots []string
		want  bool
	}{
		{"/vol1/a/b", []string{"/vol1/a"}, true},
		{"/vol1/ab", []string{"/vol1/a"}, false},
		{"/vol1/a/../b", []string{"/vol1/a"}, false},
		{"relative/path", []string{"/vol1/a"}, false},
		{"/vol1/a", []string{"/vol1/a"}, true},
		{"/vol1/a", nil, false},
	}
	for _, c := range cases {
		if got := IsUnderAny(c.path, c.roots); got != c.want {
			t.Errorf("IsUnderAny(%q, %v) = %v, want %v", c.path, c.roots, got, c.want)
		}
	}
}

func TestIsUnderAnyRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../outside", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(root, "link", "secret.txt")
	if IsUnderAny(target, []string{root}) {
		t.Fatalf("IsUnderAny(%q) followed the symlink outside the root", target)
	}
}

func TestValidateWritableACL(t *testing.T) {
	if err := ValidateWritableACL([]ACLResult{{Path: "/a", Writable: true}}); err != nil {
		t.Fatalf("writable path rejected: %v", err)
	}
	if err := ValidateWritableACL([]ACLResult{{Path: "/a", Writable: false}}); err == nil {
		t.Fatal("non-writable path accepted")
	}
	if err := ValidateWritableACL(nil); err == nil {
		t.Fatal("empty ACL accepted")
	}
}

func TestIsUnderAnyAcceptsMissingPathUnderRoot(t *testing.T) {
	root := t.TempDir()
	// A download target that does not exist yet must still count as inside the
	// granted folder, even when the root itself has a symlinked prefix.
	if !IsUnderAny(filepath.Join(root, "not-created-yet", "a.mkv"), []string{root}) {
		t.Fatalf("missing path under %q was rejected", root)
	}
}
