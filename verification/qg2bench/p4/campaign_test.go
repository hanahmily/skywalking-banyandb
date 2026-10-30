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

package p4

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	liaisongrpc "github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/query/ql"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
	vmeasure "github.com/apache/skywalking-banyandb/pkg/query/vectorized/measure"
)

func emit(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(os.Getenv("P4_OUTPUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(append(encoded, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func gcCPU() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(samples)
	return samples[0].Value.Float64()
}

func profileArm(t *testing.T, arm string, query func(int) error) {
	t.Helper()
	dir := filepath.Dir(os.Getenv("P4_OUTPUT"))
	writeProfile := func(suffix string) {
		file, err := os.Create(filepath.Join(dir, arm+suffix+".pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err = pprof.Lookup("allocs").WriteTo(file, 0); err != nil {
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	writeProfile("-before")
	file, err := os.Create(filepath.Join(dir, arm+"-cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	if err = pprof.StartCPUProfile(file); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err = query(i); err != nil {
			t.Fatal(err)
		}
	}
	pprof.StopCPUProfile()
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	assertIdle(t)
	runtime.GC()
	writeProfile("-after")
	emit(t, map[string]any{"phase": "profile", "arm": arm, "requests": 1000, "memory_profile_rate": runtime.MemProfileRate})
}

// assertIdle requires the same ownership predicates and quiet interval as the P1 probe.
func assertIdle(b *testing.T) {
	b.Helper()
	var quietSince time.Time
	for deadline := time.Now().Add(5 * time.Second); ; {
		memory := vectorized.QueryMemoryStats()
		cache := liaisongrpc.BydbQLPlanCacheStats()
		if memory.LiveReservedBytes == 0 && vmeasure.BatchExecutionStats().LiveResults == 0 && cache.InFlight == 0 &&
			cache.CompileReservedBytes == 0 && cache.UncachedBytes == 0 && vectorized.OutstandingOwnerPins() == 0 && nativemeasure.ScanResourceSnapshot().Idle() &&
			expressionIdle() {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= 20*time.Millisecond {
				return
			}
		} else {
			quietSince = time.Time{}
		}
		if time.Now().After(deadline) {
			b.Fatalf("ownership not released: memory=%+v cache=%+v scan=%+v", memory, cache, nativemeasure.ScanResourceSnapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

func memoryProbe(b *testing.T, arm string, query func() error) {
	assertIdle(b)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if !vectorized.ResetQueryMemoryPeak() {
		b.Fatal("memory live before isolated probe")
	}
	first := vectorized.QueryMemoryStats()
	for i := 0; i < 1; i++ {
		if err := query(); err != nil {
			b.Fatal(err)
		}
		assertIdle(b)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	assertIdle(b)
	record := map[string]any{
		"phase": "memory", "arm": arm, "requests": 1, "quiet_ms": 20,
		"heap_before_gc_bytes": before.HeapAlloc, "heap_after_gc_bytes": after.HeapAlloc,
		"heap_delta_bytes":        int64(after.HeapAlloc) - int64(before.HeapAlloc),
		"total_alloc_delta_bytes": after.TotalAlloc - before.TotalAlloc,
		"mallocs_delta":           after.Mallocs - before.Mallocs,
		"memory_before":           first, "memory_after": vectorized.QueryMemoryStats(),
		"scan": nativemeasure.ScanResourceSnapshot(), "batch": vmeasure.BatchExecutionStats(),
		"cache": liaisongrpc.BydbQLPlanCacheStats(), "owner_pins": vectorized.OutstandingOwnerPins(),
		"group_expression": ql.GroupExpressionExecutionStats(), "projection": ql.ExpressionExecutionStats(), "native": ql.NativeAggregateExecutionStats(),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		b.Fatal(err)
	}
	emit(b, json.RawMessage(encoded))
}

type compositionArm struct {
	requests [2]*bydbqlv1.QueryRequest
	wants    [2]string
	name     string
	native   bool
}

func fixtureAnswer(rows, lo, hi int, grouped, entity bool) string {
	sums := make([]int64, 8)
	for i := 0; i < rows; i++ {
		second := i % (rows / 8)
		if second >= lo && second < hi {
			sums[i/(rows/8)] += int64(i + 1)
		}
	}
	if entity {
		return fmt.Sprintf(":%d", sums[0])
	}
	if grouped {
		values := make([]string, 8)
		for i, n := range sums {
			values[i] = fmt.Sprintf("r%d:%d", i, n)
		}
		return strings.Join(values, ";")
	}
	var total int64
	for _, n := range sums {
		total += n
	}
	return fmt.Sprintf(":%d", total)
}

func makeRequest(base time.Time, lo, hi int, selection, suffix string) *bydbqlv1.QueryRequest {
	return &bydbqlv1.QueryRequest{
		Query: "SELECT " + selection + " FROM qg2bench.m WHERE time >= ? AND time < ?" + suffix,
		Params: []*modelv1.TagValue{
			{Value: &modelv1.TagValue_Timestamp{Timestamp: timestamppb.New(base.Add(time.Duration(lo) * time.Second))}},
			{Value: &modelv1.TagValue_Timestamp{Timestamp: timestamppb.New(base.Add(time.Duration(hi) * time.Second))}},
		},
	}
}

func runHarness(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, base time.Time, rows int) {
	if !vectorized.QueryMetricsEnabled {
		t.Fatal("querymetrics required")
	}
	specs := []struct {
		name, selection, suffix string
		native                  bool
	}{
		{"float_sum", "SUM(fv)", " AND rid = 'r0'", true},
		{"scalar_sum", "SUM(value)", "", false},
		{"group_sum", "rid, SUM(value)", " GROUP BY rid", false},
		{"float_coalesce", "COALESCE(SUM(fv),0)", " AND rid = 'r0'", true},
		{"scalar_coalesce", "COALESCE(SUM(value),0)", "", false},
		{"scalar_computed", "SUM(prior+1)", "", false},
		{"group_coalesce", "rid, COALESCE(SUM(value),0)", " GROUP BY rid", false},
		{"group_computed", "rid, SUM(prior+1)", " GROUP BY rid", false},
	}
	if os.Getenv("P4_REVISION") == "base" {
		specs = specs[:3]
	}
	mode := os.Getenv("P4_MODE")
	round, _ := strconv.Atoi(os.Getenv("P4_ROUND"))
	fixture := make([]string, rows)
	for i := range fixture {
		fixture[i] = fmt.Sprintf("r%d|%d|%d|%d|%d", i/(rows/8), i%(rows/8), i+1, i, i+1)
	}
	emit(t, map[string]any{
		"phase": "fixture", "rows": rows, "groups": 8, "records_sha256": digest(fixture),
		"base": base, "query_allowance_bytes": 268435456, "native_override": false,
	})
	// Independent grouped counts establish all eight entities are visible.
	counts, err := runQuery(ctx, client, base, "rid, COUNT(value)", " GROUP BY rid", true)
	if err != nil {
		t.Fatal(err)
	}
	expectedCounts := make([]string, 8)
	for i := range expectedCounts {
		expectedCounts[i] = fmt.Sprintf("r%d:%d", i, rows/8)
	}
	if counts != strings.Join(expectedCounts, ";") {
		t.Fatalf("group visibility: %s", counts)
	}
	for index := range specs {
		spec := specs[(index+round)%len(specs)]
		if skipArm(mode, spec.name) {
			continue
		}
		a := compositionArm{name: spec.name, native: spec.native}
		for variant := range 2 {
			lo, hi := 0, 3600
			if mode == "cache" || mode == "profile_cache" {
				lo = variant * (rows / 16)
				hi = lo + rows/16
			}
			a.requests[variant] = makeRequest(base, lo, hi, spec.selection, spec.suffix)
			a.wants[variant] = fixtureAnswer(rows, lo, hi, strings.HasPrefix(spec.name, "group"), spec.native)
		}
		emit(t, map[string]any{
			"phase": "request", "arm": a.name, "requests": a.requests, "request_sha256": digest(a.requests),
			"wants": a.wants, "oracle_sha256": digest(a.wants),
		})
		runComposition(t, ctx, client, a, mode)
	}
}

func skipArm(mode string, name string) bool {
	if mode == "cache" || mode == "profile_cache" {
		return name != "scalar_computed"
	}
	if mode == "profile" {
		return name != "scalar_sum" && name != "scalar_coalesce" && name != "scalar_computed" && name != "group_computed"
	}
	return false
}

func digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func runComposition(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, a compositionArm, mode string) {
	t.Helper()
	bypass := os.Getenv("P4_BYPASS") == "1"
	restore := liaisongrpc.SetBydbQLPlanCacheHooksForTest(liaisongrpc.PlanCacheTestHooks{ForceBypass: bypass})
	defer restore()
	query := func(index int) error {
		variant := index % 2
		got, err := runRequest(ctx, client, a.requests[variant])
		if err != nil {
			return err
		}
		if got != a.wants[variant] {
			return fmt.Errorf("%s got %s want %s", a.name, got, a.wants[variant])
		}
		return nil
	}
	if mode == "qualify" {
		before := ql.NativeAggregateExecutionStats()
		for i := 0; i < 2; i++ {
			if err := query(i); err != nil {
				emit(t, map[string]any{"phase": mode, "arm": a.name, "success": false, "error": err.Error()})
				t.Fatal(err)
			}
		}
		assertIdle(t)
		after := ql.NativeAggregateExecutionStats()
		if a.native && after.Executions-before.Executions != 2 {
			t.Fatal("eligible float missing native reduction")
		}
		emit(t, map[string]any{"phase": mode, "arm": a.name, "success": true, "requests": 2, "native_before": before, "native_after": after})
		return
	}
	for i := 0; i < 20; i++ {
		if err := query(i); err != nil {
			t.Fatal(err)
		}
	}
	assertIdle(t)
	verifyReuse(t, a.name, query, bypass)
	if mode == "memory" {
		memoryProbe(t, a.name, func() error { return query(0) })
		return
	}
	if mode == "profile" || mode == "profile_cache" {
		profileArm(t, a.name, query)
		return
	}
	samples := make([]int64, 100)
	var before, after runtime.MemStats
	cacheBefore := liaisongrpc.BydbQLPlanCacheStats()
	nativeBefore := ql.NativeAggregateExecutionStats()
	groupBefore, exprBefore := ql.GroupExpressionExecutionStats(), ql.ExpressionExecutionStats()
	runtime.ReadMemStats(&before)
	cpuStart, gcStart := cpuTime(), gcCPU()
	for i := range samples {
		started := time.Now()
		err := query(i)
		samples[i] = time.Since(started).Nanoseconds()
		if err != nil {
			emit(t, map[string]any{"phase": mode, "arm": a.name, "error": err.Error(), "samples_ns": samples[:i]})
			t.Fatal(err)
		}
	}
	cpuDelta, gcDelta := cpuTime()-cpuStart, gcCPU()-gcStart
	runtime.ReadMemStats(&after)
	assertIdle(t)
	nativeAfter := ql.NativeAggregateExecutionStats()
	if a.native && nativeAfter.Executions-nativeBefore.Executions != 100 {
		t.Fatal("timed float missing native reduction")
	}
	ordered := append([]int64(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	emit(t, map[string]any{
		"phase": mode, "arm": a.name, "bypass": bypass, "successes": 100, "samples_ns": samples, "p50_ns": ordered[49], "p95_ns": ordered[94],
		"cpu_ns_per_rpc": float64(cpuDelta.Nanoseconds()) / 100, "allocated_bytes_per_rpc": float64(after.TotalAlloc-before.TotalAlloc) / 100,
		"objects_per_rpc": float64(after.Mallocs-before.Mallocs) / 100, "gc_cpu_seconds_per_rpc": gcDelta / 100,
		"cache_before": cacheBefore, "cache_after": liaisongrpc.BydbQLPlanCacheStats(), "native_before": nativeBefore, "native_after": nativeAfter,
		"group_expression_before": groupBefore, "group_expression_after": ql.GroupExpressionExecutionStats(),
		"projection_before": exprBefore, "projection_after": ql.ExpressionExecutionStats(),
	})
}

func verifyReuse(t *testing.T, name string, query func(int) error, bypass bool) {
	before := liaisongrpc.BydbQLPlanCacheStats()
	for i := 0; i < 2; i++ {
		if err := query(i); err != nil {
			t.Fatal(err)
		}
	}
	assertIdle(t)
	after := liaisongrpc.BydbQLPlanCacheStats()
	if bypass {
		if after.Bypass-before.Bypass != 2 || after.Compiles-before.Compiles != 2 {
			t.Fatal("bypass compile mismatch")
		}
	} else if after.Hits-before.Hits != 2 || after.Compiles != before.Compiles {
		t.Fatal("cache hit mismatch")
	}
	emit(t, map[string]any{"phase": "cache_assertion", "arm": name, "bypass": bypass, "before": before, "after": after})
}

func expressionIdle() bool {
	return ql.GroupExpressionExecutionStats().LiveBytes == 0 && ql.ExpressionExecutionStats().LiveBytes == 0 && ql.NativeAggregateExecutionStats().LiveBytes == 0
}
