package fixture

import "testing"

func TestDedupeKeepsOrder(t *testing.T) {
	out := Dedupe([]string{"x", "y"})
	if out[0] != "x" {
		t.Errorf("order not preserved: %v", out)
	}
}
