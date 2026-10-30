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

package p6

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	liaisongrpc "github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/query/ql"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
	vmeasure "github.com/apache/skywalking-banyandb/pkg/query/vectorized/measure"
)

type answer struct {
	Service string `json:"service"`
	Count   int64  `json:"count"`
}

func oracle(groups int) []answer {
	result := make([]answer, 0, groups)
	// Independent rank tiers avoid reusing the engine's comparator.
	for n := 3; n >= 1; n-- {
		for g := 0; g < groups; g++ {
			if 1+g%3 == n {
				result = append(result, answer{fmt.Sprintf("s%06d", g), int64(n)})
			}
		}
	}
	return result
}

func decode(response *bydbqlv1.QueryResponse) ([]answer, error) {
	if response.GetMeasureResult() == nil {
		return nil, fmt.Errorf("missing measure result")
	}
	result := make([]answer, 0, len(response.GetMeasureResult().DataPoints))
	for _, point := range response.GetMeasureResult().DataPoints {
		if len(point.Fields) != 1 || point.Fields[0].Value.GetInt() == nil {
			return nil, fmt.Errorf("invalid fields %v", point.Fields)
		}
		service := ""
		for _, family := range point.TagFamilies {
			for _, entry := range family.Tags {
				if entry.Key == "service_id" {
					service = entry.Value.GetStr().GetValue()
				}
			}
		}
		if service == "" {
			return nil, fmt.Errorf("missing service key")
		}
		result = append(result, answer{service, point.Fields[0].Value.GetInt().Value})
	}
	return result, nil
}

func check(response *bydbqlv1.QueryResponse, want []answer) error {
	got, err := decode(response)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("rows %d != %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			return fmt.Errorf("row%d got%+v want%+v", i, got[i], want[i])
		}
	}
	return nil
}

func emit(t *testing.T, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(os.Getenv("P6_OUTPUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func save(t *testing.T, name string, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("P6_OUTPUT")), name), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func snapshot() map[string]any {
	return map[string]any{
		"memory": vectorized.QueryMemoryStats(), "scan": nativemeasure.ScanResourceSnapshot(), "cache": liaisongrpc.BydbQLPlanCacheStats(),
		"aggregate": ql.AggregateExecutionStats(), "sort": ql.SortExecutionStats(), "native": ql.NativeAggregateExecutionStats(),
		"batch": vmeasure.BatchExecutionStats(), "owner_pins": vectorized.OutstandingOwnerPins(),
	}
}

func idle(t *testing.T) {
	t.Helper()
	var quiet time.Time
	for deadline := time.Now().Add(10 * time.Second); ; {
		cache := liaisongrpc.BydbQLPlanCacheStats()
		ok := vectorized.QueryMemoryStats().LiveReservedBytes == 0 && nativemeasure.ScanResourceSnapshot().Idle() &&
			vectorized.OutstandingOwnerPins() == 0 && vmeasure.BatchExecutionStats().LiveResults == 0 && cache.InFlight == 0 &&
			cache.CompileReservedBytes == 0 && cache.UncachedBytes == 0 && ql.AggregateExecutionStats().LiveBytes == 0 &&
			ql.SortExecutionStats().LiveBytes == 0 && ql.NativeAggregateExecutionStats().LiveBytes == 0
		if ok {
			if quiet.IsZero() {
				quiet = time.Now()
			}
			if time.Since(quiet) >= 20*time.Millisecond {
				return
			}
		} else {
			quiet = time.Time{}
		}
		if time.Now().After(deadline) {
			t.Fatalf("ownership not idle: %+v", snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

func sampleOwners(done <-chan struct{}, result chan<- map[string]int64) {
	maxima := map[string]int64{"source_scan_reserved": 0, "aggregate_live": 0, "sort_live": 0, "native_live": 0, "query_live": 0}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			result <- maxima
			return
		case <-ticker.C:
			scan := nativemeasure.ScanResourceSnapshot()
			values := map[string]int64{
				"source_scan_reserved": scan.ReservedBytes, "aggregate_live": ql.AggregateExecutionStats().LiveBytes,
				"sort_live": ql.SortExecutionStats().LiveBytes, "native_live": ql.NativeAggregateExecutionStats().LiveBytes,
				"query_live": vectorized.QueryMemoryStats().LiveReservedBytes,
			}
			for key, value := range values {
				maxima[key] = max(maxima[key], value)
			}
		}
	}
}

func cpuTime() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}

func gcCPU() float64 {
	s := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(s)
	return s[0].Value.Float64()
}

func attempt(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, req *bydbqlv1.QueryRequest, want []answer, index int) bool {
	t.Helper()
	idle(t)
	runtime.GC()
	if !vectorized.ResetQueryMemoryPeak() {
		t.Fatal("peak reset failed")
	}
	before := snapshot()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	done := make(chan struct{})
	sampled := make(chan map[string]int64, 1)
	go sampleOwners(done, sampled)
	cpu0, gc0 := cpuTime(), gcCPU()
	start := time.Now()
	response, err := client.Query(ctx, req)
	elapsed := time.Since(start)
	cpuDelta, gcDelta := cpuTime()-cpu0, gcCPU()-gc0
	runtime.ReadMemStats(&m1)
	record := map[string]any{
		"phase": "export", "attempt": index, "rpc_ns": elapsed.Nanoseconds(), "cpu_ns": cpuDelta.Nanoseconds(),
		"allocated_bytes": m1.TotalAlloc - m0.TotalAlloc, "allocated_objects": m1.Mallocs - m0.Mallocs, "gc_cpu_seconds": gcDelta,
		"heap_before_gc_bytes": m0.HeapAlloc, "heap_at_response_bytes": m1.HeapAlloc, "before": before, "success": err == nil,
		"source_exact_peak_bytes": nil, "response_live_peak_bytes": nil, "count_distinct_key_capacity_bytes": nil,
	}
	if err == nil {
		if checkErr := check(response, want); checkErr != nil {
			t.Fatal(checkErr)
		}
		rows, decodeErr := decode(response)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		raw := save(t, fmt.Sprintf("output-%d.json", index), rows)
		record["decoded_rows"] = len(rows)
		record["decoded_output_bytes"] = len(raw)
		record["decoded_output_sha256"] = digest(raw)
		wire, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(response)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		record["protobuf_response_bytes"] = len(wire)
		record["protobuf_response_sha256"] = digest(wire)
	} else {
		record["error"] = err.Error()
		record["grpc_code"] = status.Code(err).String()
		if status.Code(err) != codes.ResourceExhausted {
			emit(t, record)
			t.Fatalf("noncapacity RPC error: %v", err)
		}
		boundary := "resource_admission_unspecified"
		if strings.Contains(err.Error(), "received message larger than max") {
			boundary = "client_response_limit"
		}
		record["boundary"] = boundary
	}
	idle(t)
	close(done)
	record["sampled_owner_max_bytes"] = <-sampled
	record["after"] = snapshot()
	runtime.GC()
	runtime.ReadMemStats(&m1)
	record["heap_after_gc_bytes"] = m1.HeapAlloc
	record["heap_retained_delta_bytes"] = int64(m1.HeapAlloc) - int64(m0.HeapAlloc)
	emit(t, record)
	return err == nil
}

func runExport(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, base time.Time, groups int) {
	want := oracle(groups)
	raw := save(t, "oracle.json", want)
	req := request(base, "")
	requestRaw, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	emit(t, map[string]any{"phase": "request", "request": req, "request_sha256": digest(requestRaw), "oracle_sha256": digest(raw), "output_rows": groups})
	for i := 0; i < 2; i++ {
		success := attempt(t, ctx, client, req, want, i)
		recovery, queryErr := client.Query(ctx, request(base, "s000000"))
		if queryErr == nil {
			queryErr = check(recovery, []answer{{"s000000", 1}})
		}
		if queryErr != nil {
			t.Fatalf("samecluster recovery: %v", queryErr)
		}
		idle(t)
		emit(t, map[string]any{"phase": "recovery", "attempt": i, "success": true, "owners": snapshot()})
		if success {
			if i > 0 {
				t.Fatal("capacity rejection was not reproducible; stop sweep")
			}
			break
		}
	}
	if os.Getenv("P6_DISTRIBUTED") == "1" {
		traced := request(base, "")
		traced.Query += " WITH QUERY_TRACE"
		response, queryErr := client.Query(ctx, traced)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if err = check(response, want); err != nil {
			t.Fatal(err)
		}
		input := 0
		for g := 0; g < groups; g++ {
			input += 2 * (1 + g%3)
		}
		workers := verifyWorkerRows(t, response.GetMeasureResult().GetTrace(), input)
		save(t, "worker-trace.json", response.GetMeasureResult().GetTrace())
		idle(t)
		emit(t, map[string]any{"phase": "worker_visibility", "workers": workers, "input_rows": input, "success": true})
	}
}

func TestIndependentOracle(t *testing.T) {
	want := []answer{{"s000002", 3}, {"s000001", 2}, {"s000000", 1}, {"s000003", 1}}
	got := oracle(4)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row%d got%v want%v", i, got[i], want[i])
		}
	}
}
