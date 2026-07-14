package twins

import "testing"

// Structural-twin fixture: token-identical table-test shape with
// vocabulary fully disjoint from b.go (different test name, fields,
// case labels, inputs, and failure message).
func TestParseDuration(t *testing.T) {
	cases := []struct {
		label    string
		input    string
		expected int
	}{
		{"seconds only", "45s", 45},
		{"minutes only", "7m", 420},
		{"hours only", "2h", 7200},
		{"mixed units", "1h30m", 5400},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			actual := parseDuration(c.input)
			if actual != c.expected {
				t.Errorf("parseDuration(%q) = %d, expected %d", c.input, actual, c.expected)
			}
		})
	}
}
