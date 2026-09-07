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

// Package ceildiv computes ceiling (round-up) integer division.
package ceildiv

import "fmt"

// Div returns the smallest integer greater than or equal to
// numerator/denominator — the number of fixed-size chunks of size
// denominator needed to cover numerator items. denominator must be
// positive and numerator must not be negative; either violation is
// reported as an error rather than silently dividing by (or by way
// of) a meaningless value.
func Div(numerator, denominator int) (int, error) {
	if denominator <= 0 {
		return 0, fmt.Errorf("ceildiv: denominator must be positive, got %d", denominator)
	}
	if numerator < 0 {
		return 0, fmt.Errorf("ceildiv: numerator must not be negative, got %d", numerator)
	}
	return (numerator + denominator - 1) / denominator, nil
}
