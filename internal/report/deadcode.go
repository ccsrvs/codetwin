package report

import (
	"fmt"
	"io"
)

// DeadSymbol is one display-ready dead-code finding: a definition the
// scan could not prove alive. Name follows the same "path:start-end
// symbol" format every other section uses (repo-prefixed in multi-root
// scans); Verdict is one of "dead", "unused-in-scan", "test-only".
type DeadSymbol struct {
	Name     string
	Symbol   string
	Kind     string // "function" or "class"
	Lang     string
	Exported bool
	Verdict  string
	TestRefs int
}

// deadVerdictStyle maps a verdict to its label column and color: dead
// is the high-confidence tier (red), test-only means production code
// only tests keep alive (orange), unused-in-scan is the advisory
// exported tier (yellow).
func deadVerdictStyle(verdict string) (label, clr string) {
	switch verdict {
	case "dead":
		return "DEAD            ", red
	case "test-only":
		return "TEST-ONLY       ", orange
	default:
		return "UNUSED IN SCAN  ", yellow
	}
}

// printDeadCode renders the DEAD CODE section. Findings arrive
// pre-sorted (path, then line) from the analysis; the limit was already
// applied by the caller when --limit is set.
func printDeadCode(w io.Writer, dead []DeadSymbol, opts Options) {
	if len(dead) == 0 {
		return
	}
	printSectionTitle(w, "DEAD CODE", opts)
	for _, d := range dead {
		label, clr := deadVerdictStyle(d.Verdict)
		detail := d.Kind
		if d.Exported {
			detail += ", exported"
		}
		if d.Verdict == "test-only" {
			detail += fmt.Sprintf(", %d test %s", d.TestRefs, plural(d.TestRefs, "ref", "refs"))
		}
		fmt.Fprintf(w, "  %s%s[%s]%s  %s%s%s %s(%s)%s\n",
			color(clr, opts), color(bold, opts), label, color(reset, opts),
			color(cyan, opts), d.Name, color(reset, opts),
			color(grey, opts), detail, color(reset, opts))
	}
	fmt.Fprintf(w, "\n  %sName-based reachability: conservative, but exported symbols may have\n  consumers outside this scan, and reflection-only use is invisible —\n  verify before deleting.%s\n\n",
		color(grey, opts), color(reset, opts))
}
