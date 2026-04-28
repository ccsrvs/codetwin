package similarity

import (
	"testing"

	"github.com/ccsrvs/codetwin/internal/scan"
)

func TestChunksNestedSameFile(t *testing.T) {
	cases := []struct {
		name string
		a, b scan.Snippet
		want bool
	}{
		{
			name: "outer contains closure",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 394, EndLine: 417},
			b:    scan.Snippet{Path: "/p/foo.py", StartLine: 401, EndLine: 414},
			want: true,
		},
		{
			name: "inner closure inside outer (reversed args)",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 401, EndLine: 414},
			b:    scan.Snippet{Path: "/p/foo.py", StartLine: 394, EndLine: 417},
			want: true,
		},
		{
			name: "identical ranges in same file (degenerate but counted as nested)",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 100, EndLine: 110},
			b:    scan.Snippet{Path: "/p/foo.py", StartLine: 100, EndLine: 110},
			want: true,
		},
		{
			name: "adjacent siblings — NOT nested",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 1, EndLine: 10},
			b:    scan.Snippet{Path: "/p/foo.py", StartLine: 11, EndLine: 20},
			want: false,
		},
		{
			name: "partial overlap — NOT nested (shouldn't occur in practice)",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 1, EndLine: 15},
			b:    scan.Snippet{Path: "/p/foo.py", StartLine: 10, EndLine: 20},
			want: false,
		},
		{
			name: "different files — NOT nested even when ranges contain",
			a:    scan.Snippet{Path: "/p/foo.py", StartLine: 1, EndLine: 100},
			b:    scan.Snippet{Path: "/p/bar.py", StartLine: 50, EndLine: 60},
			want: false,
		},
		{
			name: "missing path on one side — NOT nested (defensive)",
			a:    scan.Snippet{Path: "", StartLine: 1, EndLine: 100},
			b:    scan.Snippet{Path: "/p/bar.py", StartLine: 50, EndLine: 60},
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
