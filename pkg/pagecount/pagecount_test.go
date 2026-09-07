package pagecount

import "testing"

// TestPageCountBasic proves R1: PageCount returns the ceiling of
// rows/pageSize for a positive pageSize.
func TestPageCountBasic(t *testing.T) {
	got, err := PageCount(7, 2)
	if err != nil {
		t.Fatalf("PageCount(7, 2) returned error: %v", err)
	}
	if got != 4 {
		t.Errorf("PageCount(7, 2) = %d, want 4", got)
	}
}

// TestPageCountEndToEnd exercises PageCount the way a caller planning
// fixed-size batches over a result set would: how many pages of size
// 100 does 401 rows need.
func TestPageCountEndToEnd(t *testing.T) {
	got, err := PageCount(401, 100)
	if err != nil {
		t.Fatalf("PageCount(401, 100) returned error: %v", err)
	}
	if got != 5 {
		t.Errorf("PageCount(401, 100) = %d, want 5", got)
	}
}
