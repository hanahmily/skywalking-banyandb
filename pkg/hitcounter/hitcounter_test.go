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

package hitcounter

import (
	"sync"
	"testing"
)

// TestHitCounterIncrementBasic proves R1: a single Increment call raises
// Count by exactly one.
func TestHitCounterIncrementBasic(t *testing.T) {
	c := &HitCounter{}
	c.Increment()
	if got := c.Count(); got != 1 {
		t.Errorf("Count() = %d, want 1", got)
	}
}

// TestHitCounterEndToEnd exercises HitCounter the way a caller counting a
// fixed, known sequence of hits would: ten sequential increments must
// leave Count at exactly ten.
func TestHitCounterEndToEnd(t *testing.T) {
	c := &HitCounter{}
	for i := 0; i < 10; i++ {
		c.Increment()
	}
	if got := c.Count(); got != 10 {
		t.Errorf("Count() = %d, want 10", got)
	}
}

// TestHitCounterConcurrentSafe proves R2: concurrent Increment calls
// from multiple goroutines must not race on the counter's own storage.
// Deliberately absent from red_test_commands/e2e_command — proven only
// by the milestone's own race-enabled suite_command.
func TestHitCounterConcurrentSafe(t *testing.T) {
	c := &HitCounter{}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Increment()
			}
		}()
	}
	wg.Wait()
	if got := c.Count(); got != 5000 {
		t.Errorf("Count() = %d, want 5000", got)
	}
}
