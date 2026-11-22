package fnos

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsUnderAny reports whether path is inside one of roots. It rejects relative
// paths and resolves symlinks (when the target exists) so a link inside a
// granted folder cannot point outside it.
func IsUnderAny(path string, roots []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	resolved := resolvePath(clean)

	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		rootClean := filepath.Clean(root)
		rootResolved := resolvePath(rootClean)
		if resolved == rootResolved || strings.HasPrefix(resolved, rootResolved+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// resolvePath resolves symlinks as far as the path exists. A path that does
// not exist yet (a download target, a folder still being created) resolves
// its deepest existing ancestor and appends the missing tail, so a symlinked
// prefix such as /var -> /private/var still compares correctly.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir, tail := p, ""
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		tail = filepath.Join(filepath.Base(dir), tail)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, tail)
		}
	}
}

// ValidateWritableACL requires a non-empty ACL result where every path is
// writable by the user.
func ValidateWritableACL(results []ACLResult) error {
	if len(results) == 0 {
		return fmt.Errorf("fnos: empty ACL result")
	}
	for _, r := range results {
		if !r.Writable {
			return fmt.Errorf("fnos: %s is not writable for this user", r.Path)
		}
	}
	return nil
}
