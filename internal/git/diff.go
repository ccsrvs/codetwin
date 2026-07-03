package git

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
)

// LineRange is an inclusive 1-based [Start, End] line range. End >= Start.
type LineRange struct {
	Start int
	End   int
}

// DiffMap maps a repo-relative POSIX-style file path to the set of line
// ranges that differ in the post-image (working-tree) version. It's the
// authoritative "which lines does this PR touch?" view consumed by the
// --since filter.
type DiffMap map[string][]LineRange

// ChangedSince returns the post-image line ranges that differ between
// ref and the working tree, including unstaged changes. Use it to
// answer "what does the current branch introduce relative to ref?".
//
// Pure-deletion hunks and deleted files are omitted: codetwin scans
// current files, so a region that no longer exists can never overlap a
// snippet. Renames are reported under their new path because git's
// post-image header uses the new name.
func (r *Repo) ChangedSince(ref string) (DiffMap, error) {
	out, err := r.run("diff", "--unified=0", "--no-color", "--no-ext-diff", ref, "--")
	if err != nil {
		return nil, err
	}
	return parseUnifiedDiff(out), nil
}

// Touches reports whether the inclusive line range [start, end] in
// absPath intersects any line range in the diff. absPath is resolved
// relative to repoRoot before lookup; paths outside the repo (or files
// not in the diff) return false.
func (m DiffMap) Touches(repoRoot, absPath string, start, end int) bool {
	rel, ok := relWithinRoot(repoRoot, absPath)
	if !ok {
		return false
	}
	rel = filepath.ToSlash(rel)
	ranges, ok := m[rel]
	if !ok {
		return false
	}
	for _, lr := range ranges {
		if start <= lr.End && end >= lr.Start {
			return true
		}
	}
	return false
}

// parseUnifiedDiff walks `git diff --unified=0` output and extracts the
// post-image line ranges per file. Only the `+++` header and `@@` hunk
// markers are inspected — everything else (binary markers, mode
// changes, the actual content lines) is ignored.
func parseUnifiedDiff(out []byte) DiffMap {
	m := DiffMap{}
	var currentPath string
	sc := bufio.NewScanner(bytes.NewReader(out))
	// Diff hunks can carry long lines (minified bundles, generated
	// code); bump the scanner buffer so we don't error out on them.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimPrefix(line, "+++ ")
			if p == "/dev/null" {
				currentPath = ""
				continue
			}
			currentPath = stripDiffPrefix(p)
		case strings.HasPrefix(line, "@@ ") && currentPath != "":
			if lr, ok := parseHunkHeader(line); ok {
				m[currentPath] = append(m[currentPath], lr)
			}
		}
	}
	return m
}

// stripDiffPrefix removes the conventional "b/" prefix git applies to
// post-image paths. We honour --src-prefix=/--dst-prefix= edge cases by
// only stripping a leading "b/" when present, leaving everything else
// untouched.
func stripDiffPrefix(p string) string {
	if strings.HasPrefix(p, "b/") {
		return p[2:]
	}
	return p
}

// parseHunkHeader extracts the new-side range from `@@ -a,b +c,d @@`.
// Returns (zero-value, false) when the new-side count is zero (pure
// deletion at that point) since no current-file line overlaps.
func parseHunkHeader(line string) (LineRange, bool) {
	plus := strings.Index(line, "+")
	if plus < 0 {
		return LineRange{}, false
	}
	rest := line[plus+1:]
	end := strings.IndexAny(rest, " \t")
	if end < 0 {
		return LineRange{}, false
	}
	spec := rest[:end]
	start := 0
	count := 1
	if comma := strings.Index(spec, ","); comma >= 0 {
		s, err1 := strconv.Atoi(spec[:comma])
		c, err2 := strconv.Atoi(spec[comma+1:])
		if err1 != nil || err2 != nil {
			return LineRange{}, false
		}
		start, count = s, c
	} else {
		s, err := strconv.Atoi(spec)
		if err != nil {
			return LineRange{}, false
		}
		start = s
	}
	if count == 0 {
		return LineRange{}, false
	}
	return LineRange{Start: start, End: start + count - 1}, true
}
