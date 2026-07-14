package refactor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// Placement NOTEs prepended to the helper when a Java/Elixir
// suggestion has to fall back to a file-scope append because no
// enclosing container was found around A's chunk.
const (
	javaFileScopeNote = "// NOTE: appended at file scope; move it into the appropriate Java\n" +
		"// class (or extract to a utility class) before compiling.\n"
	elixirFileScopeNote = "# NOTE: appended at file scope; Elixir defs must live inside a\n" +
		"# defmodule — move this def into the appropriate module (or\n" +
		"# extract to a shared helper module) before compiling.\n"
)

// BuildPatch produces a unified diff that adds the suggestion's
// HelperSrc to pathA. v1 patches are deliberately additive: codetwin
// doesn't rewrite the existing call sites at A or B, it just plants a
// starter helper so the human (or the Claude skill) can finish the
// refactor with full visibility on what was extracted and how A and B
// diverge.
//
// Placement depends on the language (see buildPlacedPatch): Java and
// Elixir helpers are inserted inside A's enclosing container so the
// patched file compiles as emitted; every other language appends at
// the end of the file. Returns ("", nil) when s.HelperSrc is empty
// (rejection case) — callers should check Suggestion.Note instead.
func BuildPatch(pathA string, s Suggestion) (string, error) {
	if s.HelperSrc == "" {
		return "", nil
	}
	data, err := os.ReadFile(pathA)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", pathA, err)
	}
	return buildPlacedPatch(pathA, string(data), s), nil
}

// buildPlacedPatch routes a suggestion to its insertion strategy:
//
//   - Java: insert immediately before the closing `}` of the innermost
//     class/interface/enum/record enclosing A's chunk, indented like
//     A's chunk itself (a sibling member of the same container).
//   - Elixir: insert immediately before the closing `end` of the
//     innermost defmodule enclosing A's chunk, indented like a sibling
//     def.
//   - Everything else — and the defensive Java/Elixir case where no
//     enclosing container is found (free-standing code, placement
//     metadata missing) — appends at the end of the file. The
//     fallback prepends the language's file-scope placement NOTE so
//     the "move this before compiling" contract is still flagged.
func buildPlacedPatch(pathA, fileContent string, s Suggestion) string {
	switch s.Lang {
	case tokenizer.Java:
		if line, ok := javaEnclosingTypeClose(fileContent, s.SourceStartLine); ok {
			helper := indentBlock(s.HelperSrc, lineIndent(fileContent, s.SourceStartLine))
			return buildInsertBeforePatch(pathA, fileContent, helper, line)
		}
		return buildAppendPatch(pathA, fileContent, javaFileScopeNote+s.HelperSrc)
	case tokenizer.Elixir:
		if line, ok := elixirEnclosingModuleEnd(fileContent, s.SourceStartLine); ok {
			helper := indentBlock(s.HelperSrc, lineIndent(fileContent, s.SourceStartLine))
			return buildInsertBeforePatch(pathA, fileContent, helper, line)
		}
		return buildAppendPatch(pathA, fileContent, elixirFileScopeNote+s.HelperSrc)
	}
	return buildAppendPatch(pathA, fileContent, s.HelperSrc)
}

// buildInsertBeforePatch returns a unified diff that inserts helperSrc
// immediately before the (1-based) insertBefore line of fileContent,
// with up to 3 lines of context on each side. A blank separator line
// is added above the helper when the preceding line is non-blank, so
// the helper reads like any other member. Counterpart of
// buildAppendPatch for mid-file insertion; insertion points past the
// end of the file degrade to a plain append.
func buildInsertBeforePatch(pathA, fileContent, helperSrc string, insertBefore int) string {
	trimmed := strings.TrimSuffix(fileContent, "\n")
	var fileLines []string
	if trimmed != "" {
		fileLines = strings.Split(trimmed, "\n")
	}
	if insertBefore < 1 {
		insertBefore = 1
	}
	if insertBefore > len(fileLines) {
		return buildAppendPatch(pathA, fileContent, helperSrc)
	}

	insIdx := insertBefore - 1 // 0-based index of the line pushed down
	leadStart := insIdx - 3
	if leadStart < 0 {
		leadStart = 0
	}
	lead := fileLines[leadStart:insIdx]
	trailEnd := insIdx + 3
	if trailEnd > len(fileLines) {
		trailEnd = len(fileLines)
	}
	trail := fileLines[insIdx:trailEnd]

	helperLines := strings.Split(strings.TrimRight(helperSrc, "\n"), "\n")
	needSep := len(lead) > 0 && strings.TrimSpace(lead[len(lead)-1]) != ""

	oldStart := leadStart + 1
	oldLen := len(lead) + len(trail)
	newLen := oldLen + len(helperLines)
	if needSep {
		newLen++
	}

	rel := strings.TrimPrefix(filepath.ToSlash(pathA), "/")
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", rel)
	fmt.Fprintf(&b, "+++ b/%s\n", rel)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldLen, oldStart, newLen)
	for _, l := range lead {
		b.WriteString(" ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	if needSep {
		b.WriteString("+\n")
	}
	for _, l := range helperLines {
		b.WriteString("+")
		b.WriteString(l)
		b.WriteString("\n")
	}
	for _, l := range trail {
		b.WriteString(" ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
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
