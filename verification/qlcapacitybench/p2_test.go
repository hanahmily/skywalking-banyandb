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

package qlcapacitybench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
	vmeasure "github.com/apache/skywalking-banyandb/pkg/query/vectorized/measure"
	measuredata "github.com/apache/skywalking-banyandb/test/cases/measure/data"
	"github.com/apache/skywalking-banyandb/test/cases/measure/qlgen"
)

type p2Dimension struct {
	Name       string
	R, D, G, W int
}

func p2Dimensions() []p2Dimension {
	return []p2Dimension{
		{"d1000", 50000, 1000, 8, 16},
		{"d10000", 50000, 10000, 8, 16},
		{"d50000", 50000, 50000, 8, 16},
		{"r10000", 10000, 1000, 8, 16},
		{"r100000", 100000, 1000, 8, 16},
		{"g1", 50000, 1000, 1, 16},
		{"g64", 50000, 1000, 64, 16},
		{"w256", 50000, 1000, 8, 256},
	}
}

func p2Int(n int64) *modelv1.FieldValue {
	return &modelv1.FieldValue{Value: &modelv1.FieldValue_Int{Int: &modelv1.Int{Value: n}}}
}

func p2String(s string) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_Str{Str: &modelv1.Str{Value: s}}}
}

func p2Key(i, w int) string {
	s := fmt.Sprintf("key-%08d", i)
	return s + strings.Repeat("x", w-len(s))
}

func p2Write(t *testing.T, conn *grpc.ClientConn, base time.Time, dim p2Dimension) string {
	t.Helper()
	schema := proto.Clone(qlgen.P1FixtureSchema()).(*databasev1.Measure)
	schema.Metadata.Name = "p2_" + dim.Name
	_, err := databasev1.NewMeasureRegistryServiceClient(conn).Create(t.Context(), &databasev1.MeasureRegistryServiceCreateRequest{Measure: schema})
	if err != nil {
		t.Fatal(err)
	}
	// The first write also acts as schema discovery readiness. Retrying the same
	// timestamp is idempotent; the complete fixture is never rewritten during timing.
	fixtureHash := sha256.New()
	send := func() error {
		fixtureHash.Reset()
		stream, openErr := measurev1.NewMeasureServiceClient(conn).Write(t.Context())
		if openErr != nil {
			return openErr
		}
		for row := 0; row < dim.R; row++ {
			key := row % dim.D
			req := &measurev1.WriteRequest{MessageId: uint64(row + 1), DataPointSpec: &measurev1.DataPointSpec{
				TagFamilySpec: []*measurev1.TagFamilySpec{{Name: "storage-only", TagNames: []string{"entity_id"}}, {Name: "searchable", TagNames: []string{"service_id"}}},
				FieldNames:    []string{"value", "summation", "count"},
			}, DataPoint: &measurev1.DataPointValue{
				Timestamp: timestamppb.New(base.Add(time.Duration(row) * time.Millisecond)),
				TagFamilies: []*modelv1.TagFamilyForWrite{
					{Tags: []*modelv1.TagValue{p2String(fmt.Sprintf("group-%03d", key%dim.G))}},
					{Tags: []*modelv1.TagValue{p2String(p2Key(key, dim.W))}},
				},
				Fields: []*modelv1.FieldValue{p2Int(1), p2Int(1), p2Int(1)},
			}}
			identity, _ := proto.MarshalOptions{Deterministic: true}.Marshal(req)
			fixtureHash.Write(identity)
			if row == 0 {
				req.Metadata = schema.Metadata
			}
			if sendErr := stream.Send(req); sendErr != nil {
				return sendErr
			}
		}
		if closeErr := stream.CloseSend(); closeErr != nil {
			return closeErr
		}
		acks := 0
		for {
			ack, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				return recvErr
			}
			if ack.Status != modelv1.Status_STATUS_SUCCEED.String() {
				return fmt.Errorf("write %s", ack.Status)
			}
			acks++
		}
		if acks != dim.R {
			return fmt.Errorf("acks %d want %d", acks, dim.R)
		}
		return nil
	}
	for deadline := time.Now().Add(time.Minute); ; {
		err = send()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return hex.EncodeToString(fixtureHash.Sum(nil))
}

// The oracle enumerates the declared key domain and assigns its multiplicities;
// it does not read storage, parse QL, or merge worker partial results.
func p2Oracle(dim p2Dimension) map[string]int64 {
	answer := map[string]int64{}
	for key := 0; key < dim.D; key++ {
		group := fmt.Sprintf("group-%03d", key%dim.G)
		answer[group]++
	}
	return answer
}

func p2Request(base time.Time, dim p2Dimension) *bydbqlv1.QueryRequest {
	return &bydbqlv1.QueryRequest{
		Query: fmt.Sprintf("SELECT entity_id, COUNT(DISTINCT service_id) AS n FROM qlperf.p2_%s "+
			"WHERE time >= ? AND time < ? GROUP BY entity_id ORDER BY entity_id ASC", dim.Name),
		Params: []*modelv1.TagValue{timestamp(base), timestamp(base.Add(time.Duration(dim.R) * time.Millisecond))},
	}
}

func p2Call(ctx context.Context, client bydbqlv1.BydbQLServiceClient, req *bydbqlv1.QueryRequest, expected map[string]int64) error {
	response, err := client.Query(ctx, req)
	if err != nil {
		return err
	}
	if response.GetMeasureResult() == nil {
		return fmt.Errorf("no measure response")
	}
	actual := map[string]int64{}
	for _, point := range response.GetMeasureResult().DataPoints {
		fields := point.Fields
		if len(fields) != 1 || fields[0].Name != "n" || fields[0].Value.GetInt() == nil ||
			len(point.TagFamilies) != 1 || point.TagFamilies[0].Name != "storage-only" || len(point.TagFamilies[0].Tags) != 1 ||
			point.TagFamilies[0].Tags[0].Key != "entity_id" || point.TagFamilies[0].Tags[0].Value.GetStr() == nil {
			return fmt.Errorf("unexpected result %v", point)
		}
		key := point.TagFamilies[0].Tags[0].Value.GetStr().Value
		if _, exists := actual[key]; exists {
			return fmt.Errorf("duplicate group %s", key)
		}
		actual[key] = fields[0].Value.GetInt().Value
	}
	if !maps.Equal(actual, expected) {
		return fmt.Errorf("wrong result %v want %v", actual, expected)
	}
	return nil
}

func TestP2RPC(t *testing.T) {
	mode := os.Getenv("P2_MODE")
	if mode == "" {
		t.Skip("use p2_run.py")
	}
	if !vectorized.QueryMetricsEnabled {
		t.Fatal("requires querymetrics")
	}
	if mode == "memory" {
		runtime.MemProfileRate = 4096
	}
	conn, base := startFixture(t)
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	output, err := os.Create(os.Getenv("P2_OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	enc := json.NewEncoder(output)
	record := func(v any) {
		t.Helper()
		if encodeErr := enc.Encode(v); encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if syncErr := output.Sync(); syncErr != nil {
			t.Fatal(syncErr)
		}
	}
	record(map[string]any{
		"phase": "environment", "cgroup": cgroupSnapshot(), "go": runtime.Version(),
		"gomaxprocs": runtime.GOMAXPROCS(0), "query_allowance_mib": 256, "dimensions": p2Dimensions(),
	})
	defer func() { record(map[string]any{"phase": "environment_end", "cgroup": cgroupSnapshot()}) }()
	p2CombinedControl(t, conn, base, record)
	admitted := strings.Split(os.Getenv("P2_ADMITTED"), ",")
	round, _ := strconv.Atoi(os.Getenv("P2_ROUND"))
	dims := p2Dimensions()
	if mode == "round" {
		if round < 0 || round > 4 {
			t.Fatal("round 0..4")
		}
		dims = append(dims[round:], dims[:round]...)
	}
	var control *bydbqlv1.QueryRequest
	var controlOracle map[string]int64
	boundary := false
	for _, dim := range dims {
		if mode == "round" && !slices.Contains(admitted, dim.Name) {
			continue
		}
		if mode != "round" && boundary && (dim.Name == "d10000" || dim.Name == "d50000") {
			record(map[string]any{"phase": "not_run", "arm": dim.Name, "reason": "first D boundary reached"})
			continue
		}
		fixtureDigest := p2Write(t, conn, base, dim)
		req, want := p2Request(base, dim), p2Oracle(dim)
		payload, _ := proto.MarshalOptions{Deterministic: true}.Marshal(req)
		wantBytes, marshalErr := json.Marshal(want)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		record(map[string]any{
			"phase": "fixture", "arm": dim.Name, "dimensions": dim, "fixture_digest": fixtureDigest,
			"request_digest": digest(payload), "oracle_digest": digest(wantBytes), "query": req.Query,
		})
		p2WaitVisible(t, client, req, dim)
		assertReleased(t)
		if !vectorized.ResetQueryMemoryPeak() {
			t.Fatal("peak reset live")
		}
		if mode == "memory" {
			p2Profile(t, dim.Name+"-before")
		}
		beforeMemory := vectorized.QueryMemoryStats()
		beforeBatch := vmeasure.BatchExecutionStats()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		cpuStart := cpuTime()
		queryErr := p2Call(t.Context(), client, req, want)
		memory := assertReleased(t)
		cpuEnd := cpuTime()
		memory.LeaseChargedBytes -= beforeMemory.LeaseChargedBytes
		runtime.ReadMemStats(&after)
		batch := vmeasure.BatchExecutionStats()
		batch.NativeInputRows -= beforeBatch.NativeInputRows
		batch.NativeFrameRows -= beforeBatch.NativeFrameRows
		batch.NativeLeafCalls -= beforeBatch.NativeLeafCalls
		if mode == "memory" {
			p2Profile(t, dim.Name+"-after")
		}
		obs := observation{
			Phase: "feasibility", Arm: dim.Name, Memory: memory, Scan: nativemeasure.ScanResourceSnapshot(), Batch: batch,
			CPUPerRPC:       float64(cpuEnd - cpuStart),
			AllocatedPerRPC: float64(after.TotalAlloc - before.TotalAlloc), Pools: queryPools(),
		}
		if queryErr != nil {
			if status.Code(queryErr) != codes.ResourceExhausted {
				record(map[string]any{"phase": "defect", "arm": dim.Name, "error": queryErr.Error()})
				t.Fatal(queryErr)
			}
			obs.Rejections = 1
			obs.Error = queryErr.Error()
			record(obs)
			// Exactly one reproducibility probe; errors never become timing samples.
			repeatErr := p2Call(t.Context(), client, req, want)
			assertReleased(t)
			if status.Code(repeatErr) != codes.ResourceExhausted {
				t.Fatalf("nonreproducible boundary %s: %v", dim.Name, repeatErr)
			}
			record(map[string]any{"phase": "boundary_confirmation", "arm": dim.Name, "error": repeatErr.Error()})
			if strings.HasPrefix(dim.Name, "d") {
				boundary = true
			}
			if control != nil {
				if recoveryErr := p2Call(t.Context(), client, control, controlOracle); recoveryErr != nil {
					t.Fatal(recoveryErr)
				}
				assertReleased(t)
				record(map[string]any{"phase": "recovery", "arm": dim.Name, "successes": 1})
			}
			continue
		}
		obs.Successes = 1
		record(obs)
		if control == nil {
			control, controlOracle = req, want
		}
		if mode == "round" {
			p2TimedArm(t, client, req, want, dim, round, record)
		}
	}
	if mode == "memory" {
		p2MemoryLifecycle(t, client, conn, base, control, controlOracle, record)
	}
}

func p2Lifecycle(t *testing.T, client bydbqlv1.BydbQLServiceClient, req *bydbqlv1.QueryRequest, want map[string]int64, record func(any)) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	before := vmeasure.BatchExecutionStats().NativeLeafCalls
	done := make(chan error, 1)
	go func() { done <- p2Call(ctx, client, req, want) }()
	underway := false
	for !underway {
		select {
		case err := <-done:
			t.Fatalf("probe completed before underway cancellation: %v", err)
		default:
		}
		underway = vmeasure.BatchExecutionStats().NativeLeafCalls > before
		if !underway {
			time.Sleep(100 * time.Microsecond)
		}
	}
	cancel()
	err := <-done
	if status.Code(err) != codes.Canceled {
		t.Fatalf("underway cancellation: %v", err)
	}
	record(map[string]any{
		"phase": "cancellation", "underway": underway, "error": fmt.Sprint(err),
		"query_memory": assertReleased(t), "native_scan": nativemeasure.ScanResourceSnapshot(),
	})
	if recoveryErr := p2Call(t.Context(), client, req, want); recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	assertReleased(t)
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() { <-start; results <- p2Call(t.Context(), client, req, want) }()
	}
	close(start)
	successes, rejections := 0, 0
	for range 8 {
		resultErr := <-results
		switch {
		case resultErr == nil:
			successes++
		case status.Code(resultErr) == codes.ResourceExhausted:
			rejections++
		default:
			t.Fatal(resultErr)
		}
	}
	record(map[string]any{
		"phase": "eight_callers", "successes": successes, "rejections": rejections,
		"query_memory": assertReleased(t), "native_scan": nativemeasure.ScanResourceSnapshot(),
	})
	if recoveryErr := p2Call(t.Context(), client, req, want); recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	assertReleased(t)
	record(map[string]any{"phase": "lifecycle_recovery", "successes": 1})
}

// Profiling is isolated from timing; before/after allocation profiles expose
// actual call stacks without equating sampled heap with tracked reservations.
func p2Profile(t *testing.T, name string) {
	t.Helper()
	runtime.GC()
	directory := filepath.Join(filepath.Dir(os.Getenv("P2_OUTPUT")), "profiles")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(directory, name+".allocs.pprof"))
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

func p2CombinedControl(t *testing.T, conn *grpc.ClientConn, base time.Time, record func(any)) {
	t.Helper()
	suite, err := qlgen.Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range suite.ShardAggregateScenarios {
		for _, c := range scenario.Cases {
			if c.Name != "p2_combined_0_selected" {
				continue
			}
			query, renderErr := qlgen.Render(c.Query, base)
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			if _, queryErr := measuredata.VerifyQL(t.Context(), conn, query, c.WantJSON, c.Ordered); queryErr != nil {
				t.Fatal(queryErr)
			}
			assertReleased(t)
			record(map[string]any{"phase": "combined_control", "case": c.Name, "successes": 1, "query_digest": digest([]byte(query)), "oracle_digest": digest(c.WantJSON)})
			return
		}
	}
	t.Fatal("missing generated P2 combined control")
}

// The broad SUM control retains its timestamp frontier and demonstrates native
// rejection cleanup independently of admitted count-only capacity arms.
func p2RejectionControl(t *testing.T, conn *grpc.ClientConn, base time.Time, record func(any)) {
	t.Helper()
	suite, err := qlgen.Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range suite.ShardAggregateScenarios {
		for _, c := range scenario.Cases {
			if c.Name != "p2_combined_0_all" {
				continue
			}
			query, renderErr := qlgen.Render(c.Query, base)
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			assertReleased(t)
			if !vectorized.ResetQueryMemoryPeak() {
				t.Fatal("peak reset with live reservations")
			}
			batchBefore := vmeasure.BatchExecutionStats()
			_, queryErr := measuredata.VerifyQL(t.Context(), conn, query, c.WantJSON, c.Ordered)
			if status.Code(queryErr) != codes.ResourceExhausted {
				t.Fatalf("broad combined rejection: %v", queryErr)
			}
			memory := assertReleased(t)
			batchAfter := vmeasure.BatchExecutionStats()
			record(map[string]any{
				"phase": "rejection_cleanup", "case": c.Name, "error": queryErr.Error(),
				"query_memory": memory, "native_scan": nativemeasure.ScanResourceSnapshot(), "pools": queryPools(),
				"native_input_rows": batchAfter.NativeInputRows - batchBefore.NativeInputRows,
			})
			return
		}
	}
	t.Fatal("missing generated P2 rejection control")
}

func p2WaitVisible(t *testing.T, client bydbqlv1.BydbQLServiceClient, req *bydbqlv1.QueryRequest, dim p2Dimension) {
	t.Helper()
	// Native row visibility is checked independently of aggregate admission.
	visibility := proto.Clone(req).(*bydbqlv1.QueryRequest)
	visibility.Query = fmt.Sprintf("SELECT COUNT(entity_id) AS n FROM qlperf.p2_%s WHERE time >= ? AND time < ?", dim.Name)
	for deadline := time.Now().Add(time.Minute); ; {
		response, queryErr := client.Query(t.Context(), visibility)
		if queryErr == nil && checkResult(response, fmt.Sprintf(":%d", dim.R)) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture visibility %s: %v response=%v", dim.Name, queryErr, response)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func p2TimedArm(t *testing.T, client bydbqlv1.BydbQLServiceClient, req *bydbqlv1.QueryRequest, want map[string]int64, dim p2Dimension, round int, record func(any)) {
	t.Helper()

	for i := 0; i < 3; i++ {
		if warmErr := p2Call(t.Context(), client, req, want); warmErr != nil {
			t.Fatal(warmErr)
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	cpuStart := cpuTime()
	timed := observation{Phase: "round", Arm: dim.Name, Round: round}
	for i := 0; i < 30; i++ {
		start := time.Now()
		if callErr := p2Call(t.Context(), client, req, want); callErr != nil {
			record(timed)
			t.Fatal(callErr)
		}
		timed.SamplesNS = append(timed.SamplesNS, time.Since(start).Nanoseconds())
		timed.Successes++
	}
	timed.CPUPerRPC = float64(cpuTime()-cpuStart) / 30
	runtime.ReadMemStats(&after)
	timed.AllocatedPerRPC = float64(after.TotalAlloc-before.TotalAlloc) / 30
	timed.Memory = assertReleased(t)
	record(timed)
}

func p2MemoryLifecycle(t *testing.T, client bydbqlv1.BydbQLServiceClient, conn *grpc.ClientConn, base time.Time,
	control *bydbqlv1.QueryRequest, controlOracle map[string]int64, record func(any),
) {
	t.Helper()

	p2RejectionControl(t, conn, base, record)
	p2CombinedControl(t, conn, base, record)
	if control != nil {
		p2Lifecycle(t, client, control, controlOracle, record)
	} else {
		record(map[string]any{"phase": "lifecycle_skipped", "reason": "no admitted size"})
	}
}
