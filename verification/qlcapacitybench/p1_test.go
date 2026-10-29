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
	"os"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	liaisongrpc "github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/panicdiag"
	"github.com/apache/skywalking-banyandb/pkg/pool"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
	vmeasure "github.com/apache/skywalking-banyandb/pkg/query/vectorized/measure"
	"github.com/apache/skywalking-banyandb/pkg/test"
	testflags "github.com/apache/skywalking-banyandb/pkg/test/flags"
	"github.com/apache/skywalking-banyandb/pkg/test/helpers"
	"github.com/apache/skywalking-banyandb/pkg/test/setup"
	measuredata "github.com/apache/skywalking-banyandb/test/cases/measure/data"
	"github.com/apache/skywalking-banyandb/test/cases/measure/qlgen"
)

type arm struct {
	name      string
	requests  [2]*bydbqlv1.QueryRequest
	expected  [2]string
	bypass    bool
	listBytes int
}

type observation struct {
	Pools           map[string]int                  `json:"pool_refs,omitempty"`
	Phase           string                          `json:"phase"`
	Arm             string                          `json:"arm"`
	QueryDigest     string                          `json:"query_digest,omitempty"`
	Error           string                          `json:"error,omitempty"`
	BindingDigests  []string                        `json:"binding_digests,omitempty"`
	ResultDigests   []string                        `json:"result_digests,omitempty"`
	SamplesNS       []int64                         `json:"samples_ns,omitempty"`
	Round           int                             `json:"round"`
	RequestBytes    int                             `json:"request_bytes,omitempty"`
	ListBytes       int                             `json:"list_bytes,omitempty"`
	Successes       int                             `json:"successes,omitempty"`
	Rejections      int                             `json:"rejections,omitempty"`
	CPUPerRPC       float64                         `json:"cpu_ns_per_rpc,omitempty"`
	AllocatedPerRPC float64                         `json:"allocated_bytes_per_rpc,omitempty"`
	ObjectsPerRPC   float64                         `json:"allocated_objects_per_rpc,omitempty"`
	GCCPUPerRPC     float64                         `json:"gc_cpu_seconds_per_rpc,omitempty"`
	Memory          vectorized.MemoryCounters       `json:"query_memory"`
	Batch           vmeasure.BatchExecutionCounters `json:"batch"`
	BeforeHeap      uint64                          `json:"before_gc_heap,omitempty"`
	AfterHeap       uint64                          `json:"after_gc_heap,omitempty"`
	CacheBefore     liaisongrpc.PlanCacheStats      `json:"cache_before"`
	CacheAfter      liaisongrpc.PlanCacheStats      `json:"cache_after"`
	Underway        bool                            `json:"underway,omitempty"`
	Scan            nativemeasure.ScanResources     `json:"native_scan"`
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func timestamp(t time.Time) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_Timestamp{Timestamp: timestamppb.New(t)}}
}

func array(ids []string) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_StrArray{StrArray: &modelv1.StrArray{Value: ids}}}
}

func arms(base time.Time) []arm {
	var result []arm
	for _, negated := range []bool{false, true} {
		for _, size := range []int{10, 1000, 10000} {
			name, want := "in", ":32"
			if negated {
				name, want = "not_in", ":4064"
			}
			a := arm{name: fmt.Sprintf("%s_%d", name, size), expected: [2]string{want, want}}
			for variant := range 2 {
				ids := qlgen.P1List(size, 8)
				if variant == 1 {
					for i := 0; i < 8; i++ {
						ids[i] = fmt.Sprintf("present-%04d", i+8)
					}
				}
				a.requests[variant] = &bydbqlv1.QueryRequest{
					Query:  qlgen.P1ListQuery(negated),
					Params: []*modelv1.TagValue{timestamp(base), timestamp(base.Add(2 * time.Second)), array(ids)},
				}
				if variant == 0 {
					for _, id := range ids {
						a.listBytes += len(id)
					}
				}
			}
			result = append(result, a)
		}
	}
	for _, branches := range []int{2, 16} {
		a := arm{name: fmt.Sprintf("case_%d", branches), expected: [2]string{"group-a:1024;group-b:3072", "group-a:3072;group-b:1024"}}
		for variant, name := range []string{"normal", "swap"} {
			req := &bydbqlv1.QueryRequest{Query: qlgen.P1CaseQuery(branches)}
			for _, ids := range qlgen.P1Memberships(branches, name) {
				req.Params = append(req.Params, array(ids))
				if variant == 0 {
					for _, id := range ids {
						a.listBytes += len(id)
					}
				}
			}
			req.Params = append(req.Params, timestamp(base), timestamp(base.Add(2*time.Second)))
			a.requests[variant] = req
		}
		result = append(result, a)
	}
	for _, i := range []int{0, 6} {
		a := result[i]
		a.name += "_bypass"
		a.bypass = true
		result = append(result, a)
	}
	return result
}

func checkResult(response *bydbqlv1.QueryResponse, expected string) error {
	if response.GetMeasureResult() == nil {
		return fmt.Errorf("missing measure result")
	}
	var values []string
	for _, point := range response.GetMeasureResult().DataPoints {
		key := ""
		var count int64
		if len(point.TagFamilies) != 0 {
			return fmt.Errorf("unexpected tags: %v", point)
		}
		fields := point.Fields
		if len(fields) == 2 {
			if fields[0].Name != "category" || fields[0].Value.GetStr() == nil {
				return fmt.Errorf("invalid category: %v", point)
			}
			key = fields[0].Value.GetStr().GetValue()
			fields = fields[1:]
		}
		if len(fields) != 1 || fields[0].Name != "n" || fields[0].Value.GetInt() == nil {
			return fmt.Errorf("invalid fields: %v", point)
		}
		count = fields[0].Value.GetInt().GetValue()
		values = append(values, fmt.Sprintf("%s:%d", key, count))
	}
	actual := strings.Join(values, ";")
	if actual != expected {
		return fmt.Errorf("result %q, want %q", actual, expected)
	}
	return nil
}

func TestP1HarnessRejectsWrongResults(t *testing.T) {
	response := &bydbqlv1.QueryResponse{Result: &bydbqlv1.QueryResponse_MeasureResult{MeasureResult: &measurev1.QueryResponse{DataPoints: []*measurev1.DataPoint{
		{Fields: []*measurev1.DataPoint_Field{{Name: "n", Value: &modelv1.FieldValue{Value: &modelv1.FieldValue_Int{Int: &modelv1.Int{Value: 31}}}}}},
	}}}}
	if checkResult(response, ":32") == nil {
		t.Fatal("wrong distinct result accepted")
	}
	a := arms(qlgen.P1BaseTime())
	if len(a) != 10 {
		t.Fatalf("arms=%d", len(a))
	}
	for _, entry := range a {
		if proto.Equal(entry.requests[0], entry.requests[1]) {
			t.Fatalf("unchanged binding: %s", entry.name)
		}
	}
}

func gcCPU() float64 {
	samples := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(samples)
	return samples[0].Value.Float64()
}

func cgroupSnapshot() map[string]string {
	values := map[string]string{}
	for _, name := range []string{"cpu.max", "cpu.stat", "memory.max", "memory.swap.max", "memory.current", "memory.peak", "memory.events"} {
		data, readErr := os.ReadFile("/sys/fs/cgroup/" + name)
		if readErr == nil {
			values[name] = strings.TrimSpace(string(data))
		} else {
			values[name] = readErr.Error()
		}
	}
	return values
}

func cpuTime() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}

func writeFixture(ctx context.Context, conn *grpc.ClientConn, base time.Time) error {
	stream, err := measurev1.NewMeasureServiceClient(conn).Write(ctx)
	if err != nil {
		return err
	}
	for i, req := range qlgen.P1FixtureWrites(base) {
		if i > 0 {
			req.Metadata = nil
		}
		if err = stream.Send(req); err != nil {
			return err
		}
	}
	if err = stream.CloseSend(); err != nil {
		return err
	}
	count := 0
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
		count++
	}
	if count != 10240 {
		return fmt.Errorf("acknowledgements=%d", count)
	}
	return nil
}

func startFixture(t *testing.T) (*grpc.ClientConn, time.Time) {
	t.Helper()
	gomega.RegisterTestingT(t)
	testflags.LogLevel = "error"
	if err := logger.Init(logger.Logging{Env: "dev", Level: "error"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ports, err := test.AllocateFreePorts(5)
	if err != nil {
		t.Fatal(err)
	}
	addr, _, stop := setup.EmptyClosableStandalone(setup.PropertyClusterConfig(setup.NewDiscoveryFileWriter(dir)), dir, ports, "--measure-vectorized-query-memory-mib=256")
	t.Cleanup(stop)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	_, err = databasev1.NewGroupRegistryServiceClient(conn).Create(ctx, &databasev1.GroupRegistryServiceCreateRequest{Group: qlgen.P1FixtureGroup()})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range qlgen.P1IndexRules() {
		_, err = databasev1.NewIndexRuleRegistryServiceClient(conn).Create(ctx, &databasev1.IndexRuleRegistryServiceCreateRequest{IndexRule: rule})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = databasev1.NewIndexRuleBindingRegistryServiceClient(conn).Create(ctx,
		&databasev1.IndexRuleBindingRegistryServiceCreateRequest{IndexRuleBinding: qlgen.P1IndexBinding()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = databasev1.NewMeasureRegistryServiceClient(conn).Create(ctx, &databasev1.MeasureRegistryServiceCreateRequest{Measure: qlgen.P1FixtureSchema()})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)
	suite, generateErr := qlgen.Generate()
	if generateErr != nil {
		t.Fatal(generateErr)
	}
	for deadline := time.Now().Add(time.Minute); ; {
		err = writeFixture(ctx, conn, base)
		if err == nil {
			for _, c := range suite.CapacityReadiness[0].Cases {
				query, renderErr := qlgen.Render(c.Query, base)
				if renderErr != nil {
					t.Fatal(renderErr)
				}
				_, err = measuredata.VerifyQL(ctx, conn, query, c.WantJSON, c.Ordered)
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture readiness: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	return conn, base
}

func assertReleased(t *testing.T) vectorized.MemoryCounters {
	t.Helper()
	// A canceled client RPC can return before its server handler finishes.
	// Require a quiet interval, then return the snapshot that satisfied the check.
	var quietSince time.Time
	for deadline := time.Now().Add(5 * time.Second); ; {
		memory := vectorized.QueryMemoryStats()
		cache := liaisongrpc.BydbQLPlanCacheStats()
		if memory.LiveReservedBytes == 0 && vmeasure.BatchExecutionStats().LiveResults == 0 && cache.InFlight == 0 &&
			cache.CompileReservedBytes == 0 && cache.UncachedBytes == 0 && vectorized.OutstandingOwnerPins() == 0 && nativemeasure.ScanResourceSnapshot().Idle() {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= 20*time.Millisecond {
				return memory
			}
		} else {
			quietSince = time.Time{}
		}
		if time.Now().After(deadline) {
			t.Fatalf("ownership not released: memory=%+v cache=%+v batch=%+v", memory, cache, vmeasure.BatchExecutionStats())
		}
		time.Sleep(time.Millisecond)
	}
}

func queryPools() map[string]int {
	result := map[string]int{}
	for key, value := range pool.AllRefsCount() {
		if strings.Contains(key, "query") || strings.Contains(key, "scan") || strings.Contains(key, "batch") || key == "measure-blockCursor" {
			result[key] = value
		}
	}
	return result
}

func call(ctx context.Context, client bydbqlv1.BydbQLServiceClient, a arm, variant int) error {
	response, err := client.Query(ctx, a.requests[variant])
	if err != nil {
		return err
	}
	return checkResult(response, a.expected[variant])
}

func TestP1RPC(t *testing.T) {
	mode := os.Getenv("P1_MODE")
	if mode == "" {
		t.Skip("use run.py correctness or measure")
	}
	if !vectorized.QueryMetricsEnabled {
		t.Fatal("requires -tags=querymetrics")
	}
	conn, base := startFixture(t)
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	output, err := os.Create(os.Getenv("P1_OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	if err = encoder.Encode(map[string]any{
		"phase": "environment", "go_version": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "cgroup": cgroupSnapshot(),
		"fixture_digest": qlgen.P1FixtureDigest(), "query_allowance_mib": 256,
		"process_protector": "default cgroup-derived; no allowed-bytes override",
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if encodeErr := encoder.Encode(map[string]any{"phase": "environment_end", "cgroup": cgroupSnapshot()}); encodeErr != nil {
			t.Error(encodeErr)
		}
	}()
	record := func(value observation) {
		t.Helper()
		if err = encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		if err = output.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	entries := arms(base)
	round, _ := strconv.Atoi(os.Getenv("P1_ROUND"))
	if round < 0 || round > 4 {
		t.Fatal("round must be 0..4")
	}
	if mode == "correctness" {
		suite, generateErr := qlgen.Generate()
		if generateErr != nil {
			t.Fatal(generateErr)
		}
		for _, scenario := range suite.CapacityScenarios {
			for _, bypass := range []bool{false, true} {
				restore := liaisongrpc.SetBydbQLPlanCacheHooksForTest(liaisongrpc.PlanCacheTestHooks{ForceBypass: bypass})
				for _, c := range scenario.Cases {
					query, renderErr := qlgen.Render(c.Query, base)
					if renderErr != nil {
						t.Fatal(renderErr)
					}
					_, err = measuredata.VerifyQL(t.Context(), conn, query, c.WantJSON, c.Ordered)
					if err != nil {
						restore()
						t.Fatal(c.Name, err)
					}
					assertReleased(t)
					record(observation{Phase: "correctness", Arm: c.Name, Successes: 1})
				}
				restore()
			}
		}
		return
	}
	if mode == "memory" {
		memoryPass(t, client, entries, record)
		return
	}
	if mode != "feasibility" && mode != "round" {
		t.Fatal("unknown P1_MODE")
	}
	runArms(t, client, entries, mode, round, record)
}

func runArms(t *testing.T, client bydbqlv1.BydbQLServiceClient, entries []arm, mode string, round int, record func(observation)) {
	t.Helper()
	var err error
	control := entries[0]
	// Rotate complete arm order once per fresh process; request bindings alternate equally.
	entries = append(entries[round*2:], entries[:round*2]...)
	allowed := strings.Split(os.Getenv("P1_ADMITTED"), ",")
	for _, a := range entries {
		if mode == "round" && !slices.Contains(allowed, a.name) {
			continue
		}
		restore := liaisongrpc.SetBydbQLPlanCacheHooksForTest(liaisongrpc.PlanCacheTestHooks{ForceBypass: a.bypass})
		obs := observation{
			Phase: mode, Arm: a.name, Round: round, RequestBytes: proto.Size(a.requests[0]),
			ListBytes: a.listBytes, QueryDigest: digest([]byte(a.requests[0].Query)),
		}
		for _, request := range a.requests {
			bytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
			obs.BindingDigests = append(obs.BindingDigests, digest(bytes))
		}
		for _, expected := range a.expected {
			obs.ResultDigests = append(obs.ResultDigests, digest([]byte(expected)))
		}
		if mode == "feasibility" {
			for variant := range 2 {
				err = call(t.Context(), client, a, variant)
				assertReleased(t)
				if err == nil {
					obs.Successes++
				} else {
					if status.Code(err) != codes.ResourceExhausted {
						restore()
						t.Fatalf("%s unexpected defect/error: %v", a.name, err)
					}
					obs.Rejections++
					obs.Error = err.Error()
				}
			}
			record(obs)
			restore()
			// Recovery remains on this cluster, with the same unmodified resource settings.
			if obs.Rejections > 0 {
				err = call(t.Context(), client, control, 0)
				assertReleased(t)
				recovery := observation{Phase: "recovery", Arm: a.name, Successes: 1}
				if err != nil {
					recovery.Successes = 0
					recovery.Error = err.Error()
					record(recovery)
					t.Fatal(err)
				}
				record(recovery)
			}
			continue
		}
		for i := 0; i < 20; i++ {
			if err = call(t.Context(), client, a, i%2); err != nil {
				restore()
				t.Fatalf("warmup %s: %v", a.name, err)
			}
		}
		// Cache accounting is verified outside timed intervals and with changed bindings.
		obs.CacheBefore = liaisongrpc.BydbQLPlanCacheStats()
		for variant := range 2 {
			if err = call(t.Context(), client, a, variant); err != nil {
				t.Fatal(err)
			}
		}
		obs.CacheAfter = liaisongrpc.BydbQLPlanCacheStats()
		if a.bypass {
			if obs.CacheAfter.Bypass-obs.CacheBefore.Bypass != 2 || obs.CacheAfter.Compiles-obs.CacheBefore.Compiles != 2 {
				t.Fatalf("bypass did not compile: %+v", obs)
			}
		} else if obs.CacheAfter.Hits-obs.CacheBefore.Hits != 2 || obs.CacheAfter.Compiles != obs.CacheBefore.Compiles {
			t.Fatalf("cache did not hit: %+v", obs)
		}
		var before, after runtime.MemStats
		batchStart := vmeasure.BatchExecutionStats()
		runtime.ReadMemStats(&before)
		cpuStart := cpuTime()
		gcStart := gcCPU()
		for i := 0; i < 200; i++ {
			start := time.Now()
			err = call(t.Context(), client, a, i%2)
			elapsed := time.Since(start)
			if err != nil {
				obs.Error = err.Error()
				record(obs)
				restore()
				t.Fatalf("timed %s[%d]: %v", a.name, i, err)
			}
			obs.SamplesNS = append(obs.SamplesNS, elapsed.Nanoseconds())
			obs.Successes++
		}
		obs.CPUPerRPC = float64(cpuTime()-cpuStart) / 200
		runtime.ReadMemStats(&after)
		obs.AllocatedPerRPC = float64(after.TotalAlloc-before.TotalAlloc) / 200
		obs.ObjectsPerRPC = float64(after.Mallocs-before.Mallocs) / 200
		obs.GCCPUPerRPC = (gcCPU() - gcStart) / 200
		obs.Memory = assertReleased(t)
		obs.Scan = nativemeasure.ScanResourceSnapshot()
		obs.Scan = nativemeasure.ScanResourceSnapshot()
		obs.Batch = vmeasure.BatchExecutionStats()
		obs.Batch.NativeInputRows -= batchStart.NativeInputRows
		obs.Batch.NativeLeafCalls -= batchStart.NativeLeafCalls
		obs.Batch.NativeFrameRows -= batchStart.NativeFrameRows
		record(obs)
		restore()
	}
}

func memoryPass(t *testing.T, client bydbqlv1.BydbQLServiceClient, entries []arm, record func(observation)) {
	t.Helper()
	for _, a := range entries {
		restore := liaisongrpc.SetBydbQLPlanCacheHooksForTest(liaisongrpc.PlanCacheTestHooks{ForceBypass: a.bypass})
		if slices.Contains(strings.Split(os.Getenv("P1_ADMITTED"), ","), a.name) {
			if warmErr := call(t.Context(), client, a, 0); warmErr != nil {
				restore()
				t.Fatalf("memory warmup %s: %v", a.name, warmErr)
			}
		}

		assertReleased(t)
		cacheBefore := liaisongrpc.BydbQLPlanCacheStats()
		memoryBefore := vectorized.QueryMemoryStats()
		if !vectorized.ResetQueryMemoryPeak() {
			t.Fatal("peak reset with live reservation")
		}
		runtime.GC()
		var before, after runtime.MemStats
		batchStart := vmeasure.BatchExecutionStats()
		runtime.ReadMemStats(&before)
		pools := queryPools()
		err := call(t.Context(), client, a, 0)
		memory := assertReleased(t)
		memory.LeaseChargedBytes -= memoryBefore.LeaseChargedBytes
		cacheAfter := liaisongrpc.BydbQLPlanCacheStats()
		restore()
		runtime.GC()
		runtime.ReadMemStats(&after)
		obs := observation{
			Phase: "memory", Arm: a.name, Memory: memory, BeforeHeap: before.HeapAlloc, AfterHeap: after.HeapAlloc,
			Pools: queryPools(), CacheBefore: cacheBefore, CacheAfter: cacheAfter,
		}
		obs.Scan = nativemeasure.ScanResourceSnapshot()
		obs.Batch = vmeasure.BatchExecutionStats()
		obs.Batch.NativeLeafCalls -= batchStart.NativeLeafCalls
		obs.Batch.NativeInputRows -= batchStart.NativeInputRows
		obs.Batch.NativeFrameRows -= batchStart.NativeFrameRows
		obs.AllocatedPerRPC = float64(after.TotalAlloc - before.TotalAlloc)
		obs.ObjectsPerRPC = float64(after.Mallocs - before.Mallocs)
		for key, value := range obs.Pools {
			if value != pools[key] {
				t.Fatalf("%s retained pool %s: %d -> %d", a.name, key, pools[key], value)
			}
		}
		if err != nil {
			if status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("%s unexpected memory-pass error: %v", a.name, err)
			}
			obs.Error = err.Error()
			obs.Rejections = 1
		} else {
			obs.Successes = 1
		}
		record(obs)
	}
	// Observe actual native work before cancellation. A completed request is reported separately.
	a := entries[2]
	if name := os.Getenv("P1_PROBE_ARM"); name != "" {
		found := false
		for _, candidate := range entries {
			if candidate.name == name {
				a, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("unknown cancellation/concurrency probe arm %q", name)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	before := vmeasure.BatchExecutionStats().NativeLeafCalls
	result := make(chan error, 1)
	panicdiag.GoWithRecovery(ctx, panicdiag.RecoveryOptions{Component: "p1-cancellation"}, nil, func(_ *context.Context) { result <- call(ctx, client, a, 0) })
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
	obs := observation{Phase: "cancellation", Arm: a.name}
selectLoop:
	for {
		select {
		case err := <-result:
			if err != nil {
				obs.Error = err.Error()
			} else {
				obs.Successes = 1
			}
			break selectLoop
		case <-ticker.C:
			if vmeasure.BatchExecutionStats().NativeLeafCalls > before {
				obs.Underway = true
				cancel()
				err := <-result
				if err != nil {
					obs.Error = err.Error()
				} else {
					obs.Successes = 1
				}
				break selectLoop
			}
		case <-t.Context().Done():
			t.Fatal("no cancellation progress")
		}
	}
	obs.Memory = assertReleased(t)
	obs.Scan = nativemeasure.ScanResourceSnapshot()
	record(obs)
	if err := call(t.Context(), client, entries[0], 0); err != nil {
		t.Fatal("post cancellation recovery", err)
	}
	assertReleased(t)
	record(observation{Phase: "cancellation_recovery", Arm: entries[0].name, Successes: 1})
	concurrentProbe(t, client, a, entries[0], record)
}

// Keep fixture directive parsing covered at the same public seam as the generated suite.
func TestP1ParameterOrder(t *testing.T) {
	suite, err := qlgen.Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range suite.CapacityScenarios {
		for _, c := range scenario.Cases {
			rendered, renderErr := qlgen.Render(c.Query, qlgen.P1BaseTime())
			if renderErr != nil {
				t.Fatal(renderErr)
			}
			_, params, extractErr := helpers.ExtractQL(rendered)
			if extractErr != nil {
				t.Fatal(extractErr)
			}
			if strings.Contains(scenario.Name, "case") {
				if params[0].GetStrArray() == nil {
					t.Fatal("CASE must bind membership first")
				}
			} else if params[0].GetTimestamp() == nil {
				t.Fatal("IN must bind timestamps first")
			}
		}
	}
}

func concurrentProbe(t *testing.T, client bydbqlv1.BydbQLServiceClient, a, control arm, record func(observation)) {
	t.Helper()
	assertReleased(t)
	if !vectorized.ResetQueryMemoryPeak() {
		t.Fatal("concurrent peak reset with live reservation")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 8)
	for caller := 0; caller < 8; caller++ {
		variant := caller % 2
		panicdiag.GoWithRecovery(ctx, panicdiag.RecoveryOptions{Component: "p1-eight-callers"}, nil, func(_ *context.Context) {
			<-start
			results <- call(ctx, client, a, variant)
		})
	}
	close(start)
	obs := observation{Phase: "eight_callers", Arm: a.name}
	for caller := 0; caller < 8; caller++ {
		select {
		case err := <-results:
			if err == nil {
				obs.Successes++
				continue
			}
			if status.Code(err) != codes.ResourceExhausted {
				t.Fatalf("eight-caller unexpected error: %v", err)
			}
			obs.Rejections++
			obs.Error = err.Error()
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	obs.Memory = assertReleased(t)
	obs.Scan = nativemeasure.ScanResourceSnapshot()
	record(obs)
	if err := call(t.Context(), client, control, 0); err != nil {
		t.Fatal("eight-caller recovery", err)
	}
	assertReleased(t)
	record(observation{Phase: "eight_callers_recovery", Arm: control.name, Successes: 1})
}
