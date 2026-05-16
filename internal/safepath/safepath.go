package safepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Clean returns an absolute, cleaned path. It rejects empty input and paths
// containing ".." segments after cleaning. Callers must supply an absolute base
// root if they want anti-escape behaviour beyond plain cleaning.
func Clean(p string) (string, error) {
	if p == "" {
		return "", errors.New("safepath: path must not be empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("safepath: make absolute: %w", err)
	}
	// After Abs+Clean, a valid path should never contain ".." segments.
	// Guard explicitly against any that slip through non-standard inputs.
	for _, seg := range strings.Split(abs, string(os.PathSeparator)) {
		if seg == ".." {
			return "", fmt.Errorf("safepath: path %q contains illegal \"..\" segment", p)
		}
	}
	return abs, nil
}

// UnderRoot returns nil if p (cleaned, made absolute) is contained within root
// (also cleaned, absolute). Returns an error otherwise. Symlinks are not
// resolved — this is a syntactic guard, not a fs-level guarantee.
func UnderRoot(p, root string) error {
	cleanP, err := Clean(p)
	if err != nil {
		return err
	}
	cleanRoot, err := Clean(root)
	if err != nil {
		return fmt.Errorf("safepath: invalid root: %w", err)
	}
	// Ensure root ends with separator so that /foo/bar does not match /foo/baz.
	if !strings.HasSuffix(cleanRoot, string(os.PathSeparator)) {
		cleanRoot += string(os.PathSeparator)
	}
	if !strings.HasPrefix(cleanP+string(os.PathSeparator), cleanRoot) {
		return fmt.Errorf("safepath: path %q escapes root %q", p, root)
	}
	return nil
}
