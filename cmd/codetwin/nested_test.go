package main

import "testing"

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
