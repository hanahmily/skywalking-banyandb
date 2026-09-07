package floordiv

import "testing"

// TestFloorDivBasic proves R1: FloorDiv returns the floor of
// numerator/denominator for a positive denominator.
func TestFloorDivBasic(t *testing.T) {
	got, err := FloorDiv(7, 2)
	if err != nil {
		t.Fatalf("FloorDiv(7, 2) returned error: %v", err)
	}
	if got != 3 {
		t.Errorf("FloorDiv(7, 2) = %d, want 3", got)
	}
}

// TestFloorDivRejectsBadDenominator proves R2: a zero or
// negative denominator is reported as an error rather than
// dividing by it.
func TestFloorDivRejectsBadDenominator(t *testing.T) {
	if _, err := FloorDiv(7, 0); err == nil {
		t.Fatal("FloorDiv(7, 0) should have returned an error")
	}
	if _, err := FloorDiv(7, -1); err == nil {
		t.Fatal("FloorDiv(7, -1) should have returned an error")
	}
}

// TestFloorDivEndToEnd exercises FloorDiv the way a caller
// planning fixed-size batches over a result set would: how
// many FULL pages of size 100 does 401 rows fill.
func TestFloorDivEndToEnd(t *testing.T) {
	got, err := FloorDiv(401, 100)
	if err != nil {
		t.Fatalf("FloorDiv(401, 100) returned error: %v", err)
	}
	if got != 4 {
		t.Errorf("FloorDiv(401, 100) = %d, want 4", got)
	}
}
