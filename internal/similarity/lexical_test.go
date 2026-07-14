package similarity

import "testing"

func TestLexicalJaccard(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"alpha", "beta"}, []string{"alpha", "beta"}, 1.0},
		{"disjoint", []string{"alpha", "beta"}, []string{"delta", "gamma"}, 0.0},
		{"half overlap", []string{"alpha", "beta", "gamma"}, []string{"beta", "gamma", "zeta"}, 0.5},
		{"empty a", nil, []string{"alpha"}, 0.0},
		{"empty b", []string{"alpha"}, nil, 0.0},
		{"both empty", nil, nil, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LexicalJaccard(c.a, c.b); got != c.want {
				t.Errorf("LexicalJaccard(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
