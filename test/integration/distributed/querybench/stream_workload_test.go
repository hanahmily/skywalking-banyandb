// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package querybench

import "testing"

func TestStreamPermutationIsABijectionWithInverse(t *testing.T) {
	for _, n := range []int{10000, 100000, 1000000} {
		seen := make(map[int]struct{}, 1000)
		for idx := 0; idx < 1000; idx++ {
			rank := sqbPerm(idx, n)
			if _, dup := seen[rank]; dup {
				t.Fatalf("n=%d: rank %d repeated", n, rank)
			}
			seen[rank] = struct{}{}
			if got := sqbInversePerm(rank, n); got != idx {
				t.Fatalf("n=%d: inverse of rank %d = %d, want %d", n, rank, got, idx)
			}
		}
		last := n - 1
		if got := sqbInversePerm(sqbPerm(last, n), n); got != last {
			t.Fatalf("n=%d: inverse of the last element = %d, want %d", n, got, last)
		}
	}
}

func TestStreamOracleWindows(t *testing.T) {
	n := 10000
	timeIDs := expectedStreamIDs(streamVariant{Limit: 3, Offset: 2}, n)
	want := []string{sqbStoredElementID(n - 3), sqbStoredElementID(n - 4), sqbStoredElementID(n - 5)}
	for idx := range want {
		if timeIDs[idx] != want[idx] {
			t.Fatalf("time window id %d = %s, want %s", idx, timeIDs[idx], want[idx])
		}
	}
	if got := expectedStreamIDs(streamVariant{Limit: 5, Offset: 9998}, n); len(got) != 2 {
		t.Fatalf("a window past the end returned %d rows, want 2", len(got))
	}
	filtered := expectedStreamIDs(streamVariant{Limit: 100, Filter: true}, n)
	if len(filtered) != 100 {
		t.Fatalf("filter window has %d rows, want 100", len(filtered))
	}
	matches := 0
	for idx := 0; idx < n; idx++ {
		if sqbRegionOf(idx) == sqbFilterRegion {
			matches++
		}
	}
	// Regions advance every sqbServices elements, so the filter selects about 1% (a few whole blocks of elements).
	if matches < n/sqbRegions || matches > n/sqbRegions*3/2 {
		t.Fatalf("filter selects %d of %d elements, want about %d", matches, n, n/sqbRegions)
	}
	byKey := expectedStreamIDs(streamVariant{Limit: 2, IndexOrder: true}, n)
	if byKey[0] != sqbStoredElementID(sqbInversePerm(0, n)) || byKey[1] != sqbStoredElementID(sqbInversePerm(1, n)) {
		t.Fatalf("index order does not start at the smallest row keys: %v", byKey)
	}
}

func TestStreamVariantsCoverTheScenarioMatrix(t *testing.T) {
	all := []Scenario{ScenarioStreamTimeWindow, ScenarioStreamIndexOrder, ScenarioStreamFilter, ScenarioStreamTraced}
	variants := streamVariants(all, "")
	if len(variants) != 6+2+1+1 {
		t.Fatalf("got %d variants, want 10", len(variants))
	}
	if picked := streamVariants(all, "stream_time_window:100:0, stream_index_order:1000:0"); len(picked) != 2 {
		t.Fatalf("variant filter kept %d variants, want 2", len(picked))
	}
	cfg := Config{Scenario: "stream_time_window, stream_traced"}
	if scenarios, err := cfg.streamScenarios(); err != nil || len(scenarios) != 2 {
		t.Fatalf("scenario list parsed as %v, %v", scenarios, err)
	}
	if _, err := (Config{Scenario: "stream_nope"}).streamScenarios(); err == nil {
		t.Fatal("an unknown stream scenario was accepted")
	}
}
