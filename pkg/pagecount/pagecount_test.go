// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

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
