package ceildiv

import "testing"

// TestCeilDivBasic proves R1: Div returns the ceiling of
// numerator/denominator for a positive denominator.
func TestCeilDivBasic(t *testing.T) {
	got, err := Div(7, 2)
	if err != nil {
		t.Fatalf("Div(7, 2) returned error: %v", err)
	}
	if got != 4 {
		t.Errorf("Div(7, 2) = %d, want 4", got)
	}
}

// TestCeilDivRejectsBadDenominator proves R2: a zero or
// negative denominator is reported as an error rather than
// dividing by it.
func TestCeilDivRejectsBadDenominator(t *testing.T) {
	if _, err := Div(7, 0); err == nil {
		t.Fatal("Div(7, 0) should have returned an error")
	}
	if _, err := Div(7, -1); err == nil {
		t.Fatal("Div(7, -1) should have returned an error")
	}
}

// TestCeilDivEndToEnd exercises Div the way a caller
// planning fixed-size batches over a result set would: how
// many pages of size 100 does 401 rows need.
func TestCeilDivEndToEnd(t *testing.T) {
	got, err := Div(401, 100)
	if err != nil {
		t.Fatalf("Div(401, 100) returned error: %v", err)
	}
	if got != 5 {
		t.Errorf("Div(401, 100) = %d, want 5", got)
	}
}
