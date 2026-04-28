package main

import "testing"

func TestJsonLabel_BandsMatchTerminalClassifier(t *testing.T) {
	// JSON consumers script against the label string. If this diverges from
	// the terminal classify() in report.go, downstream filters silently
	// break — the bug that prompted adding this test was 151 pairs ≥0.85
	// all reporting "exact_clone" while the terminal split them across
	// "EXACT CLONE" (>0.95) and "NEAR CLONE" (>0.85).
	cases := []struct {
		score float64
		want  string
	}{
		{0.97, "exact_clone"},
		{0.95, "near_clone"}, // strict > 0.95
		{0.90, "near_clone"},
		{0.85, "strong_clone"}, // strict > 0.85
		{0.80, "strong_clone"},
		{0.65, "refactor_candidate"}, // strict > 0.65
		{0.50, "refactor_candidate"},
		{0.45, "weak_similarity"}, // strict > 0.45
		{0.30, "weak_similarity"},
	}
	for _, c := range cases {
		if got := jsonLabel(c.score); got != c.want {
			t.Errorf("jsonLabel(%.2f) = %q; want %q", c.score, got, c.want)
		}
	}
}

func TestChunksNestedSameFile(t *testing.T) {
	cases := []struct {
		name string
		a, b snippet
		want bool
	}{
		{
			name: "outer contains closure",
			a:    snippet{path: "/p/foo.py", startLine: 394, endLine: 417},
			b:    snippet{path: "/p/foo.py", startLine: 401, endLine: 414},
			want: true,
		},
		{
			name: "inner closure inside outer (reversed args)",
			a:    snippet{path: "/p/foo.py", startLine: 401, endLine: 414},
			b:    snippet{path: "/p/foo.py", startLine: 394, endLine: 417},
			want: true,
		},
		{
			name: "identical ranges in same file (degenerate but counted as nested)",
			a:    snippet{path: "/p/foo.py", startLine: 100, endLine: 110},
			b:    snippet{path: "/p/foo.py", startLine: 100, endLine: 110},
			want: true,
		},
		{
			name: "adjacent siblings — NOT nested",
			a:    snippet{path: "/p/foo.py", startLine: 1, endLine: 10},
			b:    snippet{path: "/p/foo.py", startLine: 11, endLine: 20},
			want: false,
		},
		{
			name: "partial overlap — NOT nested (shouldn't occur in practice)",
			a:    snippet{path: "/p/foo.py", startLine: 1, endLine: 15},
			b:    snippet{path: "/p/foo.py", startLine: 10, endLine: 20},
			want: false,
		},
		{
			name: "different files — NOT nested even when ranges contain",
			a:    snippet{path: "/p/foo.py", startLine: 1, endLine: 100},
			b:    snippet{path: "/p/bar.py", startLine: 50, endLine: 60},
			want: false,
		},
		{
			name: "missing path on one side — NOT nested (defensive)",
			a:    snippet{path: "", startLine: 1, endLine: 100},
			b:    snippet{path: "/p/bar.py", startLine: 50, endLine: 60},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chunksNestedSameFile(c.a, c.b); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
