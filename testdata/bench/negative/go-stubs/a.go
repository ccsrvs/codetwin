package fixture

import "testing"

func TestHashesEmpty(t *testing.T) {
	got := Hashes(Set{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}
