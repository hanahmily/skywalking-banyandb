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

package p3followup

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	liaisongrpc "github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	nativemeasure "github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
	vmeasure "github.com/apache/skywalking-banyandb/pkg/query/vectorized/measure"
	"github.com/apache/skywalking-banyandb/pkg/test"
	testflags "github.com/apache/skywalking-banyandb/pkg/test/flags"
	"github.com/apache/skywalking-banyandb/pkg/test/setup"
)

func cpuTime() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}

func stringValue(value string) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_Str{Str: &modelv1.Str{Value: value}}}
}

func listValue(values []string) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_StrArray{StrArray: &modelv1.StrArray{Value: values}}}
}

func TestPatternsRPC(b *testing.T) {
	if !vectorized.QueryMetricsEnabled {
		b.Fatal("querymetrics tag required")
	}
	gomega.RegisterTestingT(b)
	testflags.LogLevel = "error"
	if err := logger.Init(logger.Logging{Env: "dev", Level: "error"}); err != nil {
		b.Fatal(err)
	}
	size := 1000
	if text := os.Getenv("PATTERN_BENCH_NAMES"); text != "" {
		value, err := strconv.Atoi(text)
		if err != nil || (value != 1000 && value != 10000) {
			b.Fatal("PATTERN_BENCH_NAMES must be 1000 or 10000")
		}
		size = value
	}
	dir := b.TempDir()
	ports, err := test.AllocateFreePorts(5)
	if err != nil {
		b.Fatal(err)
	}
	addr, _, stop := setup.EmptyClosableStandalone(setup.PropertyClusterConfig(setup.NewDiscoveryFileWriter(dir)), dir, ports)
	defer stop()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			b.Error(closeErr)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	group := &commonv1.Group{}
	groupJSON := `{"metadata":{"name":"patternbench"},"catalog":"CATALOG_MEASURE",` +
		`"resource_opts":{"shard_num":1,"segment_interval":{"unit":"UNIT_DAY","num":1},` +
		`"ttl":{"unit":"UNIT_DAY","num":7},"replicas":1}}`
	if err = protojson.Unmarshal([]byte(groupJSON), group); err != nil {
		b.Fatal(err)
	}
	if _, err = databasev1.NewGroupRegistryServiceClient(conn).Create(ctx, &databasev1.GroupRegistryServiceCreateRequest{Group: group}); err != nil {
		b.Fatal(err)
	}
	meta := &commonv1.Metadata{Name: "names", Group: "patternbench"}
	schema := &databasev1.Measure{
		Metadata: meta, Interval: "1s", Entity: &databasev1.Entity{TagNames: []string{"name"}},
		TagFamilies: []*databasev1.TagFamilySpec{{Name: "default", Tags: []*databasev1.TagSpec{{Name: "name", Type: databasev1.TagType_TAG_TYPE_STRING}}}},
		Fields: []*databasev1.FieldSpec{{
			Name: "value", FieldType: databasev1.FieldType_FIELD_TYPE_INT,
			EncodingMethod:    databasev1.EncodingMethod_ENCODING_METHOD_GORILLA,
			CompressionMethod: databasev1.CompressionMethod_COMPRESSION_METHOD_ZSTD,
		}},
	}
	rule := &databasev1.IndexRule{
		Metadata: &commonv1.Metadata{Name: "name_idx", Group: meta.Group},
		Tags:     []string{"name"}, Type: databasev1.IndexRule_TYPE_INVERTED, Analyzer: "keyword", NoSort: true,
	}
	if _, err = databasev1.NewIndexRuleRegistryServiceClient(conn).Create(ctx, &databasev1.IndexRuleRegistryServiceCreateRequest{IndexRule: rule}); err != nil {
		b.Fatal(err)
	}
	binding := &databasev1.IndexRuleBinding{
		Metadata: &commonv1.Metadata{Name: "names_binding", Group: meta.Group},
		Rules:    []string{"name_idx"}, Subject: &databasev1.Subject{Name: meta.Name, Catalog: commonv1.Catalog_CATALOG_MEASURE},
		BeginAt:  timestamppb.New(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)),
		ExpireAt: timestamppb.New(time.Date(2121, 1, 1, 0, 0, 0, 0, time.UTC)),
	}
	bindingRegistry := databasev1.NewIndexRuleBindingRegistryServiceClient(conn)
	if _, err = bindingRegistry.Create(ctx, &databasev1.IndexRuleBindingRegistryServiceCreateRequest{IndexRuleBinding: binding}); err != nil {
		b.Fatal(err)
	}
	if _, err = databasev1.NewMeasureRegistryServiceClient(conn).Create(ctx, &databasev1.MeasureRegistryServiceCreateRequest{Measure: schema}); err != nil {
		b.Fatal(err)
	}
	names := make([]string, size)
	var selected []string
	for i := range names {
		names[i] = fmt.Sprintf("ordinary-%05d", i)
		if i < 8 {
			names[i] = fmt.Sprintf("item-MaTcH-%05d", i)
		}
		// Independent fixture-level selection; discovery is deliberately outside timing.
		if strings.Contains(strings.ToLower(names[i]), "match") {
			selected = append(selected, names[i])
		}
	}
	base, baseErr := time.Parse(time.RFC3339, os.Getenv("P3_BASE_TIME"))
	if baseErr != nil {
		b.Fatal(baseErr)
	}
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	request := func(predicate string, param *modelv1.TagValue) *bydbqlv1.QueryRequest {
		return &bydbqlv1.QueryRequest{
			Query:  "SELECT COUNT(DISTINCT name) AS total FROM MEASURE names IN patternbench WHERE time >= ? AND time < ? AND " + predicate,
			Params: []*modelv1.TagValue{stringValue(base.Format(time.RFC3339)), stringValue(base.Add(2 * time.Second).Format(time.RFC3339)), param},
		}
	}
	var readyErr error
	for deadline := time.Now().Add(time.Minute); ; {
		readyErr = writeNames(ctx, measurev1.NewMeasureServiceClient(conn), meta, base, names)
		if readyErr == nil {
			readyErr = verifyNamePages(ctx, client, names, request)
		}
		if readyErr == nil {
			break
		}
		if time.Now().After(deadline) {
			b.Fatal(readyErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("P3_FIXTURE base=%s names=%d selected=%d\n", base.Format(time.RFC3339), size, len(selected))

	fixtureBytes, fixtureErr := json.Marshal(names)
	if fixtureErr != nil {
		b.Fatal(fixtureErr)
	}
	emit(b, map[string]any{
		"phase": "fixture", "names": size, "digest": fmt.Sprintf("%x", sha256.Sum256(fixtureBytes)),
		"base": base.Format(time.RFC3339), "query_allowance_bytes": 268435456, "native_override": false,
	})
	arms := []patternArm{
		{request("name LIKE ?", stringValue("%MaTcH%")), "LIKE", 8, 16},
		{request("name ILIKE ?", stringValue("%match%")), "ILIKE", 8, 16},
		{request("name IN (?)", listValue(selected)), "IN", 8, 16},
		{request("name LIKE ?", stringValue("%absent%")), "LIKE_no_match", 0, 0},
		{request("name ILIKE ?", stringValue("%absent%")), "ILIKE_no_match", 0, 0},
		{request("name IN (?)", listValue([]string{"absent-name"})), "IN_no_match", 0, 0},
		{request("name = ?", stringValue(names[0])), "exact_control", 1, 2},
	}
	mode := os.Getenv("P3_MODE")
	if exactOnly(mode) {
		arms = arms[len(arms)-1:]
	}
	round, _ := strconv.Atoi(os.Getenv("PATTERN_BENCH_ROUND"))
	rotation := round % len(arms)
	arms = append(arms[rotation:], arms[:rotation]...)
	for _, a := range arms {
		if skipProfile(mode, a.name) {
			continue
		}
		p3Request(b, a.req, a.name, a.want)
		runArm(b, ctx, client, a, mode, request("name = ?", stringValue(names[1])), request("name = ?", stringValue("absent-name")))
	}
}

type patternArm struct {
	req  *bydbqlv1.QueryRequest
	name string
	want int64
	rows uint64
}

func emit(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(os.Getenv("P3_OUTPUT"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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

func runArm(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, a patternArm, mode string, changed, absent *bydbqlv1.QueryRequest) {
	t.Helper()
	bypass := os.Getenv("P3_BYPASS") == "1"
	restore := liaisongrpc.SetBydbQLPlanCacheHooksForTest(liaisongrpc.PlanCacheTestHooks{ForceBypass: bypass})
	defer restore()
	query := func(i int) error {
		req := a.req
		if mode == "cache" && i%2 == 1 {
			req = changed
		}
		return verifyCount(ctx, client, req, a.want)
	}
	if mode == "qualify" {
		qualify(t, ctx, client, a, changed)
		return
	}
	for i := 0; i < 20; i++ {
		if err := query(i); err != nil {
			t.Fatal(err)
		}
	}
	p3Idle(t)
	if mode == "memory" {
		p3MemoryProbe(t, a.name, func() error { return query(0) })
		return
	}
	if mode == "profile" {
		profileArm(t, a.name, query)
		return
	}
	if mode == "cache" {
		verifyCache(t, ctx, client, a, absent, bypass)
	}
	beforeCache := liaisongrpc.BydbQLPlanCacheStats()
	var before, after runtime.MemStats
	samples := make([]int64, 100)
	runtime.ReadMemStats(&before)
	beforeRows := vmeasure.BatchExecutionStats().NativeInputRows
	cpuStart, gcStart := cpuTime(), gcCPU()
	for i := range samples {
		started := time.Now()
		err := query(i)
		samples[i] = time.Since(started).Nanoseconds()
		if err != nil {
			emit(t, map[string]any{"phase": mode, "arm": a.name, "failed_index": i, "error": err.Error(), "samples_ns": samples[:i]})
			t.Fatal(err)
		}
	}
	cpuDelta, gcDelta := cpuTime()-cpuStart, gcCPU()-gcStart
	runtime.ReadMemStats(&after)
	p3Idle(t)
	rows := vmeasure.BatchExecutionStats().NativeInputRows - beforeRows
	if rows != a.rows*100 {
		t.Fatalf("native rows %d want %d", rows, a.rows*100)
	}
	ordered := append([]int64(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	emit(t, map[string]any{
		"phase": mode, "arm": a.name, "bypass": bypass, "successes": 100, "samples_ns": samples,
		"p50_ns": ordered[49], "p95_ns": ordered[94], "cpu_ns_per_rpc": float64(cpuDelta.Nanoseconds()) / 100,
		"allocated_bytes_per_rpc": float64(after.TotalAlloc-before.TotalAlloc) / 100, "objects_per_rpc": float64(after.Mallocs-before.Mallocs) / 100,
		"gc_cpu_seconds_per_rpc": gcDelta / 100, "native_rows": rows, "cache_before": beforeCache, "cache_after": liaisongrpc.BydbQLPlanCacheStats(),
		"dictionary_visits": nil, "expanded_terms": nil, "automaton_live_bytes": nil,
	})
}

func verifyCache(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, a patternArm, absent *bydbqlv1.QueryRequest, bypass bool) {
	before := liaisongrpc.BydbQLPlanCacheStats()
	if err := verifyCount(ctx, client, absent, 0); err != nil {
		t.Fatal(err)
	}
	if err := verifyCount(ctx, client, a.req, 1); err != nil {
		t.Fatal(err)
	}
	p3Idle(t)
	after := liaisongrpc.BydbQLPlanCacheStats()
	if bypass {
		if after.Bypass-before.Bypass != 2 || after.Compiles-before.Compiles != 2 {
			t.Fatal("bypass compile mismatch")
		}
	} else if after.Hits-before.Hits != 2 || after.Compiles != before.Compiles {
		t.Fatal("cache hit mismatch")
	}
	emit(t, map[string]any{"phase": "cache_assertion", "bypass": bypass, "before": before, "after": after, "expected_counts": []int{0, 1}})
}

func profileArm(t *testing.T, arm string, query func(int) error) {
	t.Helper()
	dir := filepath.Dir(os.Getenv("P3_OUTPUT"))
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
	for i := 0; i < 100; i++ {
		if err = query(i); err != nil {
			t.Fatal(err)
		}
	}
	pprof.StopCPUProfile()
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	p3Idle(t)
	runtime.GC()
	writeProfile("-after")
	emit(t, map[string]any{"phase": "profile", "arm": arm, "requests": 100, "memory_profile_rate": runtime.MemProfileRate})
}

func verifyNamePages(ctx context.Context, client bydbqlv1.BydbQLServiceClient, names []string,
	request func(string, *modelv1.TagValue) *bydbqlv1.QueryRequest,
) error {
	for start := 0; start < len(names); start += 128 {
		end := min(start+128, len(names))
		readiness := request("name IN (?)", listValue(names[start:end]))
		readiness.Query = strings.Replace(readiness.Query, "COUNT(DISTINCT name)", "COUNT(name)", 1)
		if err := verifyCount(ctx, client, readiness, int64((end-start)*2)); err != nil {
			return err
		}
	}
	return nil
}

func verifyCount(ctx context.Context, client bydbqlv1.BydbQLServiceClient, request *bydbqlv1.QueryRequest, want int64) error {
	response, err := client.Query(ctx, request)
	if err != nil {
		return err
	}
	points := response.GetMeasureResult().GetDataPoints()
	if len(points) != 1 || len(points[0].Fields) != 1 || points[0].Fields[0].Value.GetInt() == nil {
		return fmt.Errorf("expected one integer count, got %v", response)
	}
	if got := points[0].Fields[0].Value.GetInt().Value; got != want {
		return fmt.Errorf("count got %d, want %d", got, want)
	}
	return nil
}

func writeNames(ctx context.Context, client measurev1.MeasureServiceClient, meta *commonv1.Metadata, base time.Time, names []string) error {
	for start := 0; start < len(names); start += 128 {
		if err := writeNameBatch(ctx, client, meta, base, names[start:min(start+128, len(names))]); err != nil {
			return err
		}
	}
	return nil
}

func writeNameBatch(ctx context.Context, client measurev1.MeasureServiceClient, meta *commonv1.Metadata, base time.Time, names []string) error {
	stream, err := client.Write(ctx)
	if err != nil {
		return err
	}
	for i, name := range names {
		for second := 0; second < 2; second++ {
			request := &measurev1.WriteRequest{
				MessageId:     uint64(i*2 + second + 1),
				DataPointSpec: &measurev1.DataPointSpec{TagFamilySpec: []*measurev1.TagFamilySpec{{Name: "default", TagNames: []string{"name"}}}, FieldNames: []string{"value"}},
				DataPoint: &measurev1.DataPointValue{
					Timestamp:   timestamppb.New(base.Add(time.Duration(second) * time.Second)),
					TagFamilies: []*modelv1.TagFamilyForWrite{{Tags: []*modelv1.TagValue{stringValue(name)}}},
					Fields:      []*modelv1.FieldValue{{Value: &modelv1.FieldValue_Int{Int: &modelv1.Int{Value: 1}}}},
				},
			}
			if i == 0 && second == 0 {
				request.Metadata = meta
			}
			if err = stream.Send(request); err != nil {
				return err
			}
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
			return fmt.Errorf("write status %s", ack.Status)
		}
		count++
	}
	if count != len(names)*2 {
		return fmt.Errorf("write acknowledgements: %d", count)
	}
	return nil
}

// p3Idle requires the same ownership predicates and quiet interval as the P1 probe.
func p3Idle(b *testing.T) {
	b.Helper()
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

func p3MemoryProbe(b *testing.T, arm string, query func() error) {
	p3Idle(b)
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
		p3Idle(b)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	p3Idle(b)
	record := map[string]any{
		"phase": "memory", "arm": arm, "requests": 1, "quiet_ms": 20,
		"heap_before_gc_bytes": before.HeapAlloc, "heap_after_gc_bytes": after.HeapAlloc,
		"heap_delta_bytes":        int64(after.HeapAlloc) - int64(before.HeapAlloc),
		"total_alloc_delta_bytes": after.TotalAlloc - before.TotalAlloc,
		"mallocs_delta":           after.Mallocs - before.Mallocs,
		"memory_before":           first, "memory_after": vectorized.QueryMemoryStats(),
		"scan": nativemeasure.ScanResourceSnapshot(), "batch": vmeasure.BatchExecutionStats(),
		"cache": liaisongrpc.BydbQLPlanCacheStats(), "owner_pins": vectorized.OutstandingOwnerPins(),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		b.Fatal(err)
	}
	emit(b, json.RawMessage(encoded))
}

func p3Request(b *testing.T, req *bydbqlv1.QueryRequest, arm string, want int64) {
	b.Helper()
	requestBytes, err := protojson.Marshal(req)
	if err != nil {
		b.Fatal(err)
	}
	emit(b, map[string]any{
		"phase": "request", "arm": arm, "request": req, "request_sha256": fmt.Sprintf("%x", sha256.Sum256(requestBytes)),
		"expected": want, "result_oracle_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(strconv.FormatInt(want, 10)))),
	})
}

func exactOnly(mode string) bool { return mode == "exact" || mode == "cache" }
func skipProfile(mode, arm string) bool {
	return mode == "profile" && arm != "LIKE" && arm != "ILIKE" && arm != "exact_control"
}

func qualify(t *testing.T, ctx context.Context, client bydbqlv1.BydbQLServiceClient, a patternArm, recovery *bydbqlv1.QueryRequest) {
	t.Helper()
	for attempt := 0; attempt < 2; attempt++ {
		err := verifyCount(ctx, client, a.req, a.want)
		p3Idle(t)
		record := map[string]any{
			"phase": "qualify", "arm": a.name, "attempt": attempt, "success": err == nil, "error": nil,
			"memory": vectorized.QueryMemoryStats(), "scan": nativemeasure.ScanResourceSnapshot(),
		}
		if err != nil {
			record["error"] = err.Error()
		}
		emit(t, record)
		if err == nil {
			if attempt > 0 {
				t.Fatal("unstable admission failure")
			}
			return
		}
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatal(err)
		}
		if recoveryErr := verifyCount(ctx, client, recovery, 1); recoveryErr != nil {
			t.Fatal(recoveryErr)
		}
		p3Idle(t)
		emit(t, map[string]any{"phase": "recovery", "arm": a.name, "attempt": attempt, "success": true})
	}
}
