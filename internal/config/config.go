// Package config loads optional .codetwin.json from the working directory.
// The file lets users override built-in flag defaults, ignore specific files
// or directories from the scan, and strip lines matching regex patterns
// before tokenization.
//
// Schema (all fields optional):
//
//	{
//	  "defaults": { "threshold": 0.5, "preview": true, ... },
//	  "ignore_paths":    ["vendor/**", "*_test.go", "migrations/"],
//	  "ignore_patterns": ["^\\s*log\\.(info|debug)\\("]
//	}
//
// CLI flags always win over `defaults`. Path globs use the small subset of
// gitignore syntax described on Match: `*` (within a path component), `**`
// (across components), `?` (single character), trailing `/` for directory-
// only matches. Patterns without wildcards are treated as substring matches.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Filename is the on-disk name codetwin looks for in the working directory.
const Filename = ".codetwin.json"

// Config mirrors the on-disk JSON schema. All fields are optional; nil
// pointers in Defaults distinguish "not specified" from "set to zero".
type Config struct {
	Defaults        *Defaults `json:"defaults,omitempty"`
	IgnorePaths     []string  `json:"ignore_paths,omitempty"`
	IgnorePatterns  []string  `json:"ignore_patterns,omitempty"`
}

// Defaults overrides built-in flag defaults. Using pointers so the absence
// of a field is distinguishable from a zero value the user actually wrote.
type Defaults struct {
	Threshold    *float64 `json:"threshold,omitempty"`
	Plain        *bool    `json:"plain,omitempty"`
	JSON         *bool    `json:"json,omitempty"`
	Verbose      *bool    `json:"verbose,omitempty"`
	MinLines     *int     `json:"min_lines,omitempty"`
	Eps          *float64 `json:"eps,omitempty"`
	MinPts       *int     `json:"min_pts,omitempty"`
	Preview      *bool    `json:"preview,omitempty"`
	PreviewLines *int     `json:"preview_lines,omitempty"`
	Sort         *string  `json:"sort,omitempty"`
	Limit        *int     `json:"limit,omitempty"`
}

// Load reads .codetwin.json from dir and returns a parsed Config. When the
// file does not exist Load returns (nil, nil) — a missing config is not an
// error. Any JSON parse error is returned with the file path for context.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, Filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

// IgnoreMatcher decides whether a given file path should be skipped from
// the scan based on the user's ignore_paths. Build it once via
// CompileIgnorePaths and reuse for every path.
type IgnoreMatcher struct {
	rules []ignoreRule
}

type ignoreRule struct {
	raw     string
	re      *regexp.Regexp // non-nil when the pattern uses wildcards
	literal string         // non-empty when the pattern is a plain substring
	dirOnly bool
}

// CompileIgnorePaths turns a slice of glob/substring patterns into a
// matcher. Returns an error on the first invalid glob; valid patterns up
// to that point are discarded so the caller doesn't get a partial matcher.
//
// Pattern semantics:
//
//	leading "/"   anchored to the scan root (e.g. "/build" only matches at root)
//	trailing "/"  directory-only (only matches when isDir == true)
//	contains *?   compiled as a glob; "**/" is auto-prepended unless anchored
//	plain text    matched as a complete path component
func CompileIgnorePaths(patterns []string) (*IgnoreMatcher, error) {
	rules := make([]ignoreRule, 0, len(patterns))
	for _, p := range patterns {
		raw := p
		dirOnly := false
		if strings.HasSuffix(p, "/") {
			dirOnly = true
			p = strings.TrimSuffix(p, "/")
		}
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, "*?") {
			// Auto-prepend "**/" so non-anchored globs match anywhere in
			// the tree, mirroring gitignore behaviour for patterns that
			// contain "/" but don't start with one.
			glob := p
			switch {
			case strings.HasPrefix(glob, "/"):
				glob = strings.TrimPrefix(glob, "/")
			case !strings.HasPrefix(glob, "**/"):
				glob = "**/" + glob
			}
			re, err := globToRegex(glob)
			if err != nil {
				return nil, fmt.Errorf("ignore_paths %q: %w", raw, err)
			}
			rules = append(rules, ignoreRule{raw: raw, re: re, dirOnly: dirOnly})
		} else {
			rules = append(rules, ignoreRule{raw: raw, literal: p, dirOnly: dirOnly})
		}
	}
	return &IgnoreMatcher{rules: rules}, nil
}

// Match reports whether path should be ignored. isDir lets dir-only patterns
// (those ending in "/") apply only to directories. Plain (non-glob) patterns
// match when the literal appears as a complete path component, so "vendor"
// hits both "vendor" at the root and "src/vendor/lib" but not "myvendor".
func (m *IgnoreMatcher) Match(path string, isDir bool) bool {
	if m == nil || len(m.rules) == 0 {
		return false
	}
	normalizedPath := filepath.ToSlash(path)

	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		if r.re != nil {
			if r.re.MatchString(normalizedPath) {
				return true
			}
		} else {
			if matchPathComponent(normalizedPath, r.literal) {
				return true
			}
		}
	}
	return false
}

// matchPathComponent reports whether `name` appears as one of the
// slash-separated components in path, including possible "a/b" multi-
// component literals like "vendor/lib".
func matchPathComponent(path, name string) bool {
	if path == name {
		return true
	}
	if strings.Contains(name, "/") {
		// Multi-component literal: require slash boundaries on each side
		// so "vendor/lib" matches "src/vendor/lib/x" but not
		// "src/vendor/library".
		needle := name
		if strings.HasPrefix(path, needle+"/") {
			return true
		}
		if strings.HasSuffix(path, "/"+needle) {
			return true
		}
		if strings.Contains(path, "/"+needle+"/") {
			return true
		}
		return false
	}
	// Single component: split and compare.
	for _, c := range strings.Split(path, "/") {
		if c == name {
			return true
		}
	}
	return false
}

// globToRegex converts a gitignore-style glob to an anchored regular
// expression. Supports:
//
//	*   any run of chars except '/'
//	**  any run of chars including '/' (collapses surrounding slashes)
//	?   any single char except '/'
//
// All other regex metacharacters are escaped. The returned regex is
// anchored at both ends; callers test it against either the full path or
// the basename depending on the original pattern's structure.
func globToRegex(glob string) (*regexp.Regexp, error) {
	var sb strings.Builder
	sb.WriteString("^")

	i := 0
	for i < len(glob) {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			// "**/" → ".*" (eats following slash). Bare "**" at end → ".*".
			sb.WriteString(".*")
			i += 2
			if i < len(glob) && glob[i] == '/' {
				i++
			}
		case c == '*':
			sb.WriteString("[^/]*")
			i++
		case c == '?':
			sb.WriteString("[^/]")
			i++
		case c == '.', c == '+', c == '(', c == ')', c == '|', c == '^',
			c == '$', c == '{', c == '}', c == '[', c == ']', c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// CompileIgnorePatterns compiles each ignore_patterns entry into a regex.
// Invalid patterns are returned as a joined error so the user learns about
// every bad regex on a single run.
func CompileIgnorePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	var errs []string
	for _, p := range patterns {
		// Apply (?m) so ^ / $ anchor on each line — that's what users
		// almost always want when stripping per-line constructs like log
		// statements, and it can't be retroactively added by the user
		// without knowing the regex flavor.
		re, err := regexp.Compile("(?m)" + p)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%q: %v", p, err))
			continue
		}
		out = append(out, re)
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("invalid ignore_patterns: %s", strings.Join(errs, "; "))
	}
	return out, nil
}
