package svca

import "testing"

// TestApplyDiscountTiers is copy-pasted alongside the production clone
// into svc-b — a test↔test pair that the default test segregation must
// suppress even across repo roots.
func TestApplyDiscountTiers(t *testing.T) {
	prices := []float64{10.0, 20.0, 30.0}
	cases := []struct {
		tier int
		want float64
	}{
		{0, 60.00},
		{1, 57.00},
		{2, 54.00},
		{3, 48.00},
	}
	for _, c := range cases {
		got := ApplyDiscount(prices, c.tier)
		if got != c.want {
			t.Errorf("tier %d: got %v, want %v", c.tier, got, c.want)
		}
	}
}
