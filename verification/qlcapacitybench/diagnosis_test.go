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
// KIND, either express or implied. See the License for the
// specific language governing permissions and limitations
// under the License.

package qlcapacitybench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	liaisongrpc "github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/pool"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
)

// TestP1RepeatedRejectionMemory diagnoses retention; GC/profile work is never a latency measurement.
func TestP1RepeatedRejectionMemory(t *testing.T) {
	directory := os.Getenv("P1_DIAG_OUTPUT")
	if directory == "" {
		t.Skip("set P1_DIAG_OUTPUT inside the fixed-resource diagnostic container")
	}
	if !vectorized.QueryMetricsEnabled {
		t.Fatal("requires -tags=querymetrics")
	}
	runtime.MemProfileRate = 4096
	conn, base := startFixture(t)
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	entries := arms(base)
	output, err := os.Create(filepath.Join(directory, "retention.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = output.Close() })
	encoder := json.NewEncoder(output)
	record := func(value any) {
		t.Helper()
		if encodeErr := encoder.Encode(value); encodeErr != nil {
			t.Fatal(encodeErr)
		}
	}
	profile := func(name string) {
		t.Helper()
		file, createErr := os.Create(filepath.Join(directory, name+".heap.pprof"))
		if createErr != nil {
			t.Fatal(createErr)
		}
		writeErr := pprof.WriteHeapProfile(file)
		closeErr := file.Close()
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	record(map[string]any{
		"phase": "environment", "cgroup": cgroupSnapshot(), "mem_profile_rate": runtime.MemProfileRate,
		"go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "query_allowance_mib": 256,
	})
	assertReleased(t)
	pools := queryPools()
	checkpoint := func(phase string, iteration, collections int) {
		t.Helper()
		assertReleased(t)
		for range collections {
			runtime.GC()
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		refs := queryPools()
		for name, count := range refs {
			if count != pools[name] {
				t.Fatalf("%s retained %s: %d -> %d", phase, name, pools[name], count)
			}
		}
		record(map[string]any{
			"phase": phase, "iteration": iteration, "collections": collections, "heap_alloc": memory.HeapAlloc,
			"heap_objects": memory.HeapObjects, "heap_inuse": memory.HeapInuse, "total_alloc": memory.TotalAlloc,
			"num_gc": memory.NumGC, "query_memory": vectorized.QueryMemoryStats(), "native_scan": nativemeasure.ScanResourceSnapshot(),
			"cache": liaisongrpc.BydbQLPlanCacheStats(), "pool_refs": refs, "all_pool_refs": pool.AllRefsCount(),
		})
	}
	run := func(a arm, rejected bool) {
		t.Helper()
		if !vectorized.ResetQueryMemoryPeak() {
			t.Fatal("reset while query ownership live")
		}
		callErr := call(t.Context(), client, a, 0)
		assertReleased(t)
		if rejected && status.Code(callErr) != codes.ResourceExhausted {
			t.Fatalf("%s expected ResourceExhausted, got %v", a.name, callErr)
		}
		if !rejected && callErr != nil {
			t.Fatalf("%s recovery: %v", a.name, callErr)
		}
		message := ""
		if callErr != nil {
			message = callErr.Error()
		}
		record(map[string]any{
			"phase": "query", "arm": a.name, "rejected": rejected, "error": message,
			"query_memory": vectorized.QueryMemoryStats(), "cache": liaisongrpc.BydbQLPlanCacheStats(),
		})
	}
	// Warm the admitted path and stabilize setup garbage before comparing identical rejected calls.
	for range 3 {
		run(entries[0], false)
	}
	checkpoint("baseline", 0, 2)
	profile("baseline")
	for i := range 5 {
		time.Sleep(100 * time.Millisecond)
		checkpoint("idle_before", i, 2)
	}
	for i := range 5 {
		run(entries[0], false)
		checkpoint("admitted_control", i, 1)
		checkpoint("admitted_control_twice", i, 1)
	}
	for _, index := range []int{3, 7} {
		a := entries[index]
		// Establish the same compiled template and error path before the repeated curve.
		run(a, true)
		run(entries[0], false)
		checkpoint(a.name+"_before", 0, 2)
		profile(a.name + "_before")
		for i := range 16 {
			run(a, true)
			checkpoint(a.name, i, 1)
			if i == 0 || i == 15 {
				profile(fmt.Sprintf("%s_%02d_once", a.name, i))
			}
			checkpoint(a.name+"_twice", i, 1)
			if i == 0 || i == 15 {
				profile(fmt.Sprintf("%s_%02d_twice", a.name, i))
			}
			run(entries[0], false)
		}
		checkpoint(a.name+"_after", 0, 2)
		profile(a.name + "_after")
	}
	for i := range 5 {
		time.Sleep(100 * time.Millisecond)
		checkpoint("idle_after", i, 2)
	}
	profile("final")
	// Keep client-side request arrays live through the idle control; their death is not server cleanup.
	runtime.KeepAlive(entries)
	record(map[string]any{"phase": "environment_end", "cgroup": cgroupSnapshot()})
}
