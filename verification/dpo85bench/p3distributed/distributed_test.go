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

package p3distributed

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
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

func BenchmarkPatternsRPC(b *testing.B) {
	if !vectorized.QueryMetricsEnabled {
		b.Fatal("querymetrics tag required")
	}
	gomega.RegisterTestingT(b)
	testflags.LogLevel = "error"
	if err := logger.Init(logger.Logging{Env: "dev", Level: "error"}); err != nil {
		b.Fatal(err)
	}
	size := 10000
	dir := b.TempDir()
	b.Setenv("POD_NAME", "localhost")
	config := setup.PropertyClusterConfig(setup.NewDiscoveryFileWriter(dir))
	var nodes []string
	for i := 0; i < 2; i++ {
		nodeAddr, _, _, closeNode := setup.DataNodeWithAddrAndDir(config, "--measure-vectorized-query-memory-mib=256")
		defer closeNode()
		nodes = append(nodes, nodeAddr)
	}
	addr, stop := setup.LiaisonNode(config, "--measure-vectorized-query-memory-mib=256")
	defer stop()
	fmt.Printf("P3_NODES %v\n", nodes)
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
		`"resource_opts":{"shard_num":2,"segment_interval":{"unit":"UNIT_DAY","num":1},` +
		`"ttl":{"unit":"UNIT_DAY","num":7},"replicas":0}}`
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
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)
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
	type arm struct {
		param           *modelv1.TagValue
		name, predicate string
		want            int64
	}
	arms := []arm{
		{stringValue("%MaTcH%"), "LIKE", "name LIKE ?", 8},
		{stringValue(names[0]), "exact_control", "name = ?", 1},
		{stringValue("%MaTcH%"), "LIKE_computed_sum", "name LIKE ?", 32},
	}
	for _, a := range arms {
		req := request(a.predicate, a.param)
		if a.name == "LIKE_computed_sum" {
			req.Query = strings.Replace(req.Query, "COUNT(DISTINCT name)", "SUM(value::FIELD+1)", 1)
		}
		traceReq := &bydbqlv1.QueryRequest{Query: req.Query + " WITH QUERY_TRACE", Params: req.Params}
		traceResp, traceErr := client.Query(ctx, traceReq)
		if traceErr != nil {
			b.Fatal(traceErr)
		}
		traceJSON, traceErr := protojson.Marshal(traceResp)
		if traceErr != nil {
			b.Fatal(traceErr)
		}
		fmt.Printf("P3_TRACE arm=%s response=%s\n", a.name, traceJSON)
		validateTrace(b, traceResp, a.want, a.name != "exact_control")

		p3Request(b, req, a.name, a.want)
		if err = verifyCount(ctx, client, req, a.want); err != nil {
			b.Fatalf("warm %s: %v", a.name, err)
		}
		b.Run(fmt.Sprintf("names_%d/%s", size, a.name), func(b *testing.B) {
			if !vectorized.ResetQueryMemoryPeak() {
				b.Fatal("query memory still live before timing")
			}
			p3Idle(b)
			stopProfile := startProfile(b, a.name)
			before := vmeasure.BatchExecutionStats()
			latencies := make([]int64, 0, b.N)
			b.ReportAllocs()
			b.ResetTimer()
			startedCPU := cpuTime()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				if queryErr := verifyCount(ctx, client, req, a.want); queryErr != nil {
					b.Fatal(queryErr)
				}
				latencies = append(latencies, time.Since(start).Nanoseconds())
			}
			b.StopTimer()
			elapsedCPU := cpuTime() - startedCPU
			stopProfile()
			p3Idle(b)
			sorted := append([]int64(nil), latencies...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			raw, _ := json.Marshal(map[string]any{"arm": a.name, "latency_ns": latencies, "p50_ns": sorted[(len(sorted)-1)/2], "p95_ns": sorted[(len(sorted)*95-1)/100]})
			fmt.Printf("P3_LATENCY %s\n", raw)
			after := vmeasure.BatchExecutionStats()
			memory := vectorized.QueryMemoryStats()
			if memory.LiveReservedBytes != 0 || after.LiveResults != before.LiveResults {
				b.Fatal("query memory/result leak")
			}
			b.ReportMetric(float64(elapsedCPU.Nanoseconds())/float64(b.N), "cpu-ns/op")
			b.ReportMetric(float64(memory.PeakReservedBytes), "peak-tracked-B")
			b.ReportMetric(float64(after.NativeInputRows-before.NativeInputRows)/float64(b.N), "native-rows/op")
			p3MemoryProbe(b, a.name, func() error { return verifyCount(ctx, client, req, a.want) })
		})
	}
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
func p3Idle(b *testing.B) {
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

func p3MemoryProbe(b *testing.B, arm string, query func() error) {
	if os.Getenv("P3_MEMORY") != "1" {
		return
	}
	p3Idle(b)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if !vectorized.ResetQueryMemoryPeak() {
		b.Fatal("memory live before isolated probe")
	}
	first := vectorized.QueryMemoryStats()
	for i := 0; i < 20; i++ {
		if err := query(); err != nil {
			b.Fatal(err)
		}
		p3Idle(b)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	p3Idle(b)
	record := map[string]any{
		"arm": arm, "requests": 20, "quiet_ms": 20,
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
	fmt.Printf("P3_MEMORY %s\n", encoded)
}

func p3Request(b *testing.B, req *bydbqlv1.QueryRequest, arm string, want int64) {
	b.Helper()
	requestBytes, err := protojson.Marshal(req)
	if err != nil {
		b.Fatal(err)
	}
	fmt.Printf("P3_REQUEST arm=%s sha256=%x want=%d\n", arm, sha256.Sum256(requestBytes), want)
}

func startProfile(b *testing.B, arm string) func() {
	b.Helper()
	dir := os.Getenv("P3_PROFILE_DIR")
	if dir == "" {
		return func() {}
	}
	profile, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%d.cpu.pprof", arm, b.N)))
	if err != nil {
		b.Fatal(err)
	}
	if err = pprof.StartCPUProfile(profile); err != nil {
		b.Fatal(err)
	}
	return func() {
		pprof.StopCPUProfile()
		if err = profile.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func validateTrace(b *testing.B, response *bydbqlv1.QueryResponse, want int64, pattern bool) {
	b.Helper()
	points := response.GetMeasureResult().GetDataPoints()
	if len(points) != 1 || len(points[0].Fields) != 1 || points[0].Fields[0].Value.GetInt() == nil || points[0].Fields[0].Value.GetInt().Value != want {
		b.Fatalf("trace result differs from independent oracle: %v", response)
	}
	workers := map[string]int{}
	var walk func(*commonv1.Span, string)
	walk = func(span *commonv1.Span, worker string) {
		if strings.HasPrefix(span.Message, "data-") {
			worker = span.Message
			workers[worker] = 0
		}
		if span.Message == "frame-encode" && worker != "" {
			for _, tag := range span.Tags {
				if tag.Key == "rows_out" {
					rows, err := strconv.Atoi(tag.Value)
					if err != nil {
						b.Fatal(err)
					}
					workers[worker] += rows
				}
			}
		}
		for _, child := range span.Children {
			walk(child, worker)
		}
	}
	for _, span := range response.GetMeasureResult().GetTrace().GetSpans() {
		walk(span, "")
	}
	total := 0
	for _, rows := range workers {
		total += rows
		if pattern && rows == 0 {
			b.Fatalf("pattern did not select both workers: %v", workers)
		}
	}
	expected := 2
	if pattern {
		expected = 16
	}
	if len(workers) != 2 || total != expected {
		b.Fatalf("worker contributions: %v, expected total%d across2", workers, expected)
	}
	encoded, err := json.Marshal(workers)
	if err != nil {
		b.Fatal(err)
	}
	fmt.Printf("P3_WORKERS %s\n", encoded)
}
