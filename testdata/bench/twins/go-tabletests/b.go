package twins

import "testing"

// Structural-twin fixture: same table-test shape as a.go, disjoint
// vocabulary (custom field types keep even `string`/`int` out of the
// shared lexical set).
func TestBadgeMarkup(t *testing.T) {
	rows := []struct {
		title   text
		state   text
		classes badgeWidth
	}{
		{"passing build", "ok", 3},
		{"failing build", "err", 9},
		{"stale cache", "old", 12},
		{"queued job", "wait", 5},
	}
	for _, r := range rows {
		t.Run(r.title, func(t *testing.T) {
			markup := renderBadge(r.state)
			if markup != r.classes {
				t.Fatalf("renderBadge(%q) = %d, want %d", r.state, markup, r.classes)
			}
		})
	}
}
