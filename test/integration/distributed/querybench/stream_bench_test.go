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

package querybench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	streamv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/stream/v1"
	"github.com/apache/skywalking-banyandb/banyand/metadata/schema"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/query/vectorized"
)

func init() {
	// Sample allocations more finely so the per-query allocation delta is not lost in the default
	// 512 KiB sampling. Set before any benchmark allocation; it applies to every combo of both builds alike.
	if os.Getenv(envEngine) == engineStream && getBool(envRunBench) {
		runtime.MemProfileRate = 64 << 10
	}
}

// runStreamSingleShot runs every variant of the selected stream scenarios against one freshly written data set.
func runStreamSingleShot(t *testing.T, cfg Config) {
	t.Helper()
	if initErr := logger.Init(logger.Logging{Env: "dev", Level: "warn"}); initErr != nil {
		t.Fatalf("initialize logger: %v", initErr)
	}
	gomega.RegisterTestingT(t)
	scenarios, scenarioErr := cfg.streamScenarios()
	if scenarioErr != nil {
		t.Fatal(scenarioErr)
	}
	cluster, _, clusterErr := startSoakClusterWith(t, cfg, nil, preloadStreamQuerySchema(cfg.ShardNum, cfg.StreamReplicas), schema.KindStream)
	if clusterErr != nil {
		t.Fatalf("start cluster: %v", clusterErr)
	}
	defer cluster.close()
	n := cfg.Cardinality
	baseNanos := time.Now().UnixNano()
	if raw := os.Getenv("DQB_BASE_NANOS"); raw != "" {
		if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
			baseNanos = parsed
		}
	}
	// The newest element sits at the pinned base, so no element lies in the future.
	start := time.Unix(0, baseNanos-baseNanos%int64(time.Millisecond)).Add(-time.Duration(n) * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	written, writeErr := writeStreamQueryData(ctx, cluster.conn, cfg, n, start)
	if writeErr != nil {
		t.Fatalf("write stream data: %v", writeErr)
	}
	t.Logf("loaded engine=stream nodes=%d replicas=%d elements=%d payload=%dB duration=%s rows_per_sec=%.0f",
		cfg.DataNodes, cfg.StreamReplicas, n, cfg.StreamPayloadBytes, written.Duration, written.RowsPerSec)
	if visibleErr := waitStreamVisible(ctx, cluster.conn, n, start); visibleErr != nil {
		t.Fatalf("wait for visibility: %v", visibleErr)
	}
	settled, settleErr := waitStreamQuiescent(ctx, 10*time.Second, 3*time.Minute)
	if settleErr != nil {
		t.Fatalf("wait for the cluster to settle: %v", settleErr)
	}
	t.Logf("cluster settled after %s", settled)
	client := streamv1.NewStreamServiceClient(cluster.conn)
	if len(streamVariants(scenarios, cfg.StreamVariants)) == 0 {
		t.Fatalf("%s=%q selects no variant", envStreamVariants, cfg.StreamVariants)
	}
	for _, variant := range streamVariants(scenarios, cfg.StreamVariants) {
		result, runErr := runStreamVariant(ctx, t, client, cfg, variant, n, start)
		if result.Stream != nil {
			result.Stream.SettleSeconds = settled.Seconds()
		}
		if runErr != nil {
			// Record the failed variant and carry on, so a refused large window does not hide the other variants.
			t.Errorf("run %s/%s: %v", variant.Scenario, variant.key(), runErr)
			result.Error = runErr.Error()
			result.Correctness = "error"
			if result.Stream != nil && result.Stream.Oracle == "" {
				result.Stream.Oracle = "not-run"
			}
			if _, shardErr := writeShard(result, cfg.ReportDir); shardErr != nil {
				t.Fatalf("write shard: %v", shardErr)
			}
			continue
		}
		shardPath, shardErr := writeShard(result, cfg.ReportDir)
		if shardErr != nil {
			t.Fatalf("write shard: %v", shardErr)
		}
		t.Logf("shard written: %s (p50=%.2fms p99=%.2fms qps=%.2f rows=%d oracle=%s)", shardPath,
			result.Latency.P50Ms, result.Latency.P99Ms, result.QPS, result.ResponseRows, result.Stream.Oracle)
	}
}

func runStreamVariant(
	ctx context.Context, t *testing.T, client streamv1.StreamServiceClient, cfg Config, variant streamVariant, n int, start time.Time,
) (Result, error) {
	t.Helper()
	req := buildStreamQuery(variant, n, start)
	extra := &StreamExtra{
		DataNodes: cfg.DataNodes, Replicas: cfg.StreamReplicas, ShardNum: cfg.ShardNum, PayloadBytes: cfg.StreamPayloadBytes,
		Limit: variant.Limit, Offset: variant.Offset, Trace: variant.Trace, VecMetricsEnabled: vectorized.QueryMetricsEnabled,
		SerialIterations: cfg.StreamSerialIterations,
		BudgetNote: "the liaison's per-query lease (protector.QueryReservation) exposes no peak counter and the in-process harness cannot reach it; " +
			"vec_* are process-wide vectorized tracker counters (data-node scans), not the liaison's alignment charges",
	}
	result := Result{
		Engine: engineStream, Mode: cfg.Mode, Scenario: variant.Scenario, Variant: fmt.Sprintf("dn=%d,rep=%d,%s", cfg.DataNodes, cfg.StreamReplicas, variant.key()),
		Cardinality: n, Entities: sqbServices, ShardNum: cfg.ShardNum, DataNodes: cfg.DataNodes, SpanBytes: cfg.StreamPayloadBytes,
		QueryIterations: cfg.QueryIterations, QueryWorkers: cfg.QueryWorkers, Correctness: "baseline", Stream: extra,
	}
	query := func() (*streamv1.QueryResponse, time.Duration, error) {
		queryCtx, cancel := context.WithTimeout(ctx, queryTimeout(n))
		defer cancel()
		began := time.Now()
		resp, queryErr := client.Query(queryCtx, proto.Clone(req).(*streamv1.QueryRequest))
		return resp, time.Since(began), queryErr
	}
	for warmup := 0; warmup < cfg.WarmupIterations; warmup++ {
		if _, _, queryErr := query(); queryErr != nil {
			return result, fmt.Errorf("warmup query %d: %w", warmup, queryErr)
		}
	}
	sample, _, sampleErr := query()
	if sampleErr != nil {
		return result, fmt.Errorf("sample query: %w", sampleErr)
	}
	extra.SampleIDs = streamResponseIDs(sample)
	extra.SampleContentHash = hashStreamContent(sample)
	extra.ResponseBytesProtoSample = proto.Size(sample)
	if want := expectedStreamIDs(variant, n); slices.Equal(want, extra.SampleIDs) {
		extra.Oracle = "ok"
	} else {
		extra.Oracle = fmt.Sprintf("MISMATCH want_rows=%d got_rows=%d", len(want), len(extra.SampleIDs))
		t.Errorf("oracle mismatch for %s/%s: want %d rows, got %d", variant.Scenario, variant.key(), len(want), len(extra.SampleIDs))
	}
	if variant.Trace && sample.GetTrace() == nil {
		extra.Oracle += "; TRACE-MISSING"
		t.Errorf("traced query returned no trace")
	}
	wantRows, wantHash := len(extra.SampleIDs), hashStreamIDs(sample)
	result.ResponseRows = wantRows
	result.ApproxResultHash = extra.SampleContentHash

	// Phase A: the concurrent timed phase (latency, QPS, CPU, allocation, memory peaks).
	releaseStreamMemory()
	allocBefore, allocErr := writeAllocsProfile()
	if allocErr != nil {
		return result, allocErr
	}
	var cpuBuf bytes.Buffer
	probe := startMemProbe()
	before := captureProcessSnapshot()
	if cpuErr := pprof.StartCPUProfile(&cpuBuf); cpuErr != nil {
		return result, fmt.Errorf("start cpu profile: %w", cpuErr)
	}
	phase, phaseErr := runStreamQueries(ctx, client, req, n, cfg.QueryIterations, cfg.QueryWorkers, wantRows, wantHash)
	pprof.StopCPUProfile()
	after := captureProcessSnapshot()
	probe.finish()
	if phaseErr != nil {
		return result, phaseErr
	}
	releaseStreamMemory()
	allocAfter, allocErr := writeAllocsProfile()
	if allocErr != nil {
		return result, allocErr
	}
	allocs, diffErr := diffAllocProfiles(allocBefore, allocAfter)
	if diffErr != nil {
		return result, diffErr
	}
	cpu, cpuErr := attributeCPU(cpuBuf.Bytes())
	if cpuErr != nil {
		return result, cpuErr
	}
	queries := float64(len(phase.Latencies))
	result.Latency, result.QPS = summarizeLatencies(phase.Latencies, phase.Elapsed)
	result.RawLatenciesNS = phase.Latencies
	result.Resources = resourceDelta(before, after)
	result.Allocations = allocationDelta(before, after, len(phase.Latencies))
	extra.LiaisonAllocBytesPerQ = allocs.LiaisonBytes / queries
	extra.LiaisonAllocObjectsPerQ = allocs.LiaisonObjects / queries
	extra.ProfileAllocBytesPerQ = allocs.TotalBytes / queries
	extra.ProfileAllocObjectsPerQ = allocs.TotalObjects / queries
	extra.LiaisonCPUSeconds = cpu.LiaisonSeconds
	extra.ProfiledCPUSeconds = cpu.TotalSeconds
	extra.PeakHeapBytes, extra.BaselineHeapBytes = probe.PeakHeap, probe.BaselineHeap
	extra.PeakLiveHeapBytes, extra.BaselineLiveHeapBytes = probe.PeakLive, probe.BaselineLive
	extra.PeakRSSBytes, extra.PeakRSSPolledBytes, extra.BaselineRSSBytes = probe.PeakRSSHWM, probe.PeakRSSPolled, probe.BaselineRSS
	extra.RSSHighWaterMarkResetOK = probe.hwmResetWorked
	if cfg.Profile {
		if saveErr := saveStreamProfiles(cfg, result, cpuBuf.Bytes(), allocs); saveErr != nil {
			return result, saveErr
		}
	}

	// Phase B: unloaded serial queries (single worker) for latency without queueing and a clean per-query memory peak.
	releaseStreamMemory()
	waitForQueryMemoryIdle()
	vectorized.ResetQueryMemoryPeak()
	chargedBefore := vectorized.QueryMemoryStats().LeaseChargedBytes
	serialProbe := startMemProbe()
	serial, serialErr := runStreamQueries(ctx, client, req, n, cfg.StreamSerialIterations, 1, wantRows, wantHash)
	serialProbe.finish()
	if serialErr != nil {
		return result, serialErr
	}
	stats := vectorized.QueryMemoryStats()
	extra.SerialLatency, _ = summarizeLatencies(serial.Latencies, serial.Elapsed)
	extra.SerialLatencies = serial.Latencies
	extra.SerialPeakHeapBytes, extra.SerialBaselineHeapBytes = serialProbe.PeakHeap, serialProbe.BaselineHeap
	extra.SerialPeakLiveHeapBytes, extra.SerialBaselineLiveBytes = serialProbe.PeakLive, serialProbe.BaselineLive
	extra.VecPeakReservedBytes = stats.PeakReservedBytes
	extra.VecLeaseChargedPerQuery = float64(stats.LeaseChargedBytes-chargedBefore) / float64(max(len(serial.Latencies), 1))

	// Phase C: what the liaison's query path still holds while a serial query runs (heap-profile samples
	// attributed by allocation stack). Last, because the sampling slows the query down.
	if cfg.StreamHeapProbeQueries > 0 {
		releaseStreamMemory()
		live, liveErr := probeLiaisonLiveHeap(ctx, client, req, n, cfg.StreamHeapProbeQueries, wantRows, wantHash)
		if liveErr != nil {
			return result, fmt.Errorf("liaison heap probe: %w", liveErr)
		}
		extra.LiaisonLiveIdleBytes, extra.LiaisonLiveMaxBytes, extra.LiaisonLiveMeanBytes = live.IdleBytes, live.MaxBytes, live.MeanBytes
		extra.LiaisonLiveMaxObjects, extra.LiaisonLiveSamples, extra.LiaisonLiveSampleBytes = live.MaxObjects, live.Samples, live.SampleBytes
	}
	result.Correctness = "oracle-" + extra.Oracle
	return result, nil
}

// waitForQueryMemoryIdle waits for released vectorized reservations so the peak counter can be reset.
func waitForQueryMemoryIdle() {
	for attempt := 0; attempt < 40; attempt++ {
		if vectorized.QueryMemoryStats().LiveReservedBytes == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func saveStreamProfiles(cfg Config, result Result, cpuRaw []byte, allocs allocAttribution) error {
	dir := filepath.Join(cfg.ReportDir, "profiles", string(result.Scenario), strconv.Itoa(result.Cardinality), sanitizeProfilePath(result.Variant))
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return fmt.Errorf("create profile directory: %w", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(dir, "cpu.pprof"), cpuRaw, 0o600); writeErr != nil {
		return fmt.Errorf("write cpu profile: %w", writeErr)
	}
	file, createErr := os.Create(filepath.Join(dir, "allocs-delta.pprof"))
	if createErr != nil {
		return fmt.Errorf("create allocs delta profile: %w", createErr)
	}
	if writeErr := allocs.Merged.Write(file); writeErr != nil {
		_ = file.Close()
		return fmt.Errorf("write allocs delta profile: %w", writeErr)
	}
	return file.Close()
}
