package refactor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BuildPatch produces a unified diff that appends the suggestion's
// HelperSrc to the end of pathA. v1 patches are deliberately
// additive: codetwin doesn't rewrite the existing call sites at A or
// B, it just plants a starter helper so the human (or the Claude
// skill) can finish the refactor with full visibility on what was
// extracted and how A and B diverge.
//
// The diff uses up to 3 lines of trailing context anchored on the
// final lines of pathA so `git apply` succeeds even when the
// surrounding file has unrelated commits since the snapshot codetwin
// scanned. Returns ("", nil) when s.HelperSrc is empty (rejection
// case) — callers should check Suggestion.Note instead.
func BuildPatch(pathA string, s Suggestion) (string, error) {
	if s.HelperSrc == "" {
		return "", nil
	}
	data, err := os.ReadFile(pathA)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pathA, err)
	}
	return buildAppendPatch(pathA, string(data), s.HelperSrc), nil
}

// buildAppendPatch is BuildPatch's pure core: given the current file
// content and the helper source, return a unified diff string that
// appends the helper at the end of the file. Split out for testing
// without touching the filesystem.
//
// Trailing-empty-line handling: the file's actual line structure is
// preserved (we strip exactly one final `\n` — the file terminator —
// before splitting), so a file ending with `}\n\n` produces a final
// empty line that the hunk includes as trailing context. Without
// this, git apply's view of the file diverges from ours and the hunk
// fails to match.
func buildAppendPatch(pathA, fileContent, helperSrc string) string {
	trimmed := strings.TrimSuffix(fileContent, "\n")
	var fileLines []string
	if trimmed != "" {
		fileLines = strings.Split(trimmed, "\n")
	}
	originalLineCount := len(fileLines)

	// Anchor on the last 3 lines of the file (pre-image context). If
	// the last line happens to be empty (trailing blank line), it's
	// still valid context — git matches it as a single space + LF.
	ctxStart := originalLineCount - 3
	if ctxStart < 0 {
		ctxStart = 0
	}
	ctxLines := fileLines[ctxStart:]

	helperTrim := strings.TrimRight(helperSrc, "\n")
	helperLines := strings.Split(helperTrim, "\n")

	// Two emit modes:
	//   - File ends in a non-empty line: append helper after a blank
	//     separator line for readability.
	//   - File already ends with one or more blank lines: those act
	//     as the separator. Insert helper *before* the trailing blank
	//     line so the file's tail structure is preserved.
	insertBeforeTrailingBlank := originalLineCount > 0 &&
		fileLines[originalLineCount-1] == ""

	hunkOldStart := ctxStart + 1
	hunkOldLen := len(ctxLines)
	hunkNewStart := hunkOldStart
	hunkNewLen := hunkOldLen + len(helperLines)
	if !insertBeforeTrailingBlank {
		hunkNewLen++ // blank separator we add before the helper
	}

	// Strip a leading slash so absolute paths render as `a/foo/bar.go`
	// rather than `a//foo/bar.go` — the latter still applies cleanly
	// with `git apply -p<n>`, but the former is the normal git diff
	// shape and avoids confusing reviewers.
	rel := strings.TrimPrefix(filepath.ToSlash(pathA), "/")

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", rel)
	fmt.Fprintf(&b, "+++ b/%s\n", rel)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", hunkOldStart, hunkOldLen, hunkNewStart, hunkNewLen)

	if insertBeforeTrailingBlank {
		// All but the last context line stay as plain context; the
		// trailing blank line is preserved by emitting it after the
		// added helper lines.
		for _, l := range ctxLines[:len(ctxLines)-1] {
			b.WriteString(" ")
			b.WriteString(l)
			b.WriteString("\n")
		}
		for _, l := range helperLines {
			b.WriteString("+")
			b.WriteString(l)
			b.WriteString("\n")
		}
		b.WriteString(" \n") // trailing blank line preserved
	} else {
		for _, l := range ctxLines {
			b.WriteString(" ")
			b.WriteString(l)
			b.WriteString("\n")
		}
		b.WriteString("+\n") // blank separator before the helper
		for _, l := range helperLines {
			b.WriteString("+")
			b.WriteString(l)
			b.WriteString("\n")
		}
	}
	return b.String()
}
