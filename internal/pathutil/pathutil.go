// Package pathutil provides small lexical helpers for working with file
// system paths. All functions are pure (no I/O).
package pathutil

import (
	"path/filepath"
	"strings"
)

// Contains reports whether `child` lives inside the directory tree rooted at
// `parent`. Both paths must be absolute and clean. The check is purely
// lexical (no filesystem access): parent must be a strict prefix of child
// with the next character being a separator, so /foo does not match /foobar.
func Contains(parent, child string) bool {
	if !strings.HasPrefix(child, parent) {
		return false
	}
	rest := child[len(parent):]
	return len(rest) > 0 && rest[0] == filepath.Separator
}

// Dedupe removes duplicate inputs and inputs that are contained within
// another input on the list. Given ./src and ./src/utils, only ./src is
// returned because walking it will already cover ./src/utils. Identical
// paths (after canonicalization) are also collapsed.
//
// Order from the input slice is preserved for the survivors so users see
// their first-mentioned form in error messages and progress.
func Dedupe(paths []string) ([]string, error) {
	type entry struct {
		original string
		abs      string
	}
	var entries []entry
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(abs)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		entries = append(entries, entry{original: p, abs: abs})
	}

	out := make([]string, 0, len(entries))
	for _, ai := range entries {
		contained := false
		for _, aj := range entries {
			if aj.abs == ai.abs {
				continue
			}
			if Contains(aj.abs, ai.abs) {
				contained = true
				break
			}
		}
		if !contained {
			out = append(out, ai.original)
		}
	}
	return out, nil
}
