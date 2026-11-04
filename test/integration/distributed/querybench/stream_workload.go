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
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/pprof/profile"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	streamv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/stream/v1"
	"github.com/apache/skywalking-banyandb/banyand/metadata/schema"
	"github.com/apache/skywalking-banyandb/pkg/convert"
)

// The stream engine measures the liaison's distributed stream query merge. One test-binary invocation boots one
// cluster (data nodes x replicas), writes one data set, and then runs every window variant of the selected scenarios
// against it, one variant at a time, so each variant gets its own timed phase, CPU profile, allocation delta and
// memory peak. The workload only uses the public stream Write and Query APIs.

const (
	sqbGroup      = "bench-stream-q"
	sqbStream     = "sw"
	sqbFamilyData = "data"
	sqbFamilyTags = "searchable"

	sqbTagPayload  = "payload"
	sqbTagRowKey   = "row_key"
	sqbTagRegion   = "region"
	sqbTagService  = "service_id"
	sqbTagState    = "state"
	sqbTagDuration = "duration"

	sqbRuleRowKey = "bench-row-key"
	sqbRuleRegion = "bench-region"

	sqbServices     = 64
	sqbRegions      = 100
	sqbFilterRegion = 7
	sqbPermMul      = 7919
	sqbPermAdd      = 101
	sqbVisibleSlice = 20000
)

// streamVariant is one query shape of a stream scenario.
type streamVariant struct {
	Scenario   Scenario
	Limit      uint32
	Offset     uint32
	Trace      bool
	IndexOrder bool
	Filter     bool
}

func (v streamVariant) key() string {
	return fmt.Sprintf("L=%d,O=%d", v.Limit, v.Offset)
}

// streamScenarios parses the comma-separated scenario selection of the stream engine.
func (c Config) streamScenarios() ([]Scenario, error) {
	var out []Scenario
	for _, part := range strings.Split(string(c.Scenario), ",") {
		scenario := Scenario(strings.TrimSpace(part))
		switch scenario {
		case ScenarioStreamTimeWindow, ScenarioStreamIndexOrder, ScenarioStreamFilter, ScenarioStreamTraced:
			out = append(out, scenario)
		default:
			return nil, fmt.Errorf("%s unsupported for %s=%s: %q", envScenario, envEngine, engineStream, scenario)
		}
	}
	return out, nil
}

// streamVariants expands the scenarios into their query variants in a fixed order. A non-empty filter replaces that
// default table with the variants it lists, as comma-separated scenario:limit:offset entries in the given order; an
// entry may use a window the default table does not contain (for example a large limit).
func streamVariants(scenarios []Scenario, filter string) []streamVariant {
	if strings.TrimSpace(filter) == "" {
		return allStreamVariants(scenarios)
	}
	selected := make(map[Scenario]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		selected[scenario] = struct{}{}
	}
	var out []streamVariant
	for _, entry := range strings.Split(filter, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ":")
		if len(parts) != 3 {
			continue
		}
		limit, limitErr := strconv.ParseUint(parts[1], 10, 32)
		offset, offsetErr := strconv.ParseUint(parts[2], 10, 32)
		scenario := Scenario(parts[0])
		if _, ok := selected[scenario]; !ok || limitErr != nil || offsetErr != nil || limit == 0 {
			continue
		}
		out = append(out, streamVariant{
			Scenario: scenario, Limit: uint32(limit), Offset: uint32(offset),
			Trace:      scenario == ScenarioStreamTraced,
			IndexOrder: scenario == ScenarioStreamIndexOrder,
			Filter:     scenario == ScenarioStreamFilter,
		})
	}
	return out
}

func allStreamVariants(scenarios []Scenario) []streamVariant {
	var out []streamVariant
	for _, scenario := range scenarios {
		switch scenario {
		case ScenarioStreamTimeWindow:
			for _, limit := range []uint32{20, 100, 1000} {
				for _, offset := range []uint32{0, 100} {
					out = append(out, streamVariant{Scenario: scenario, Limit: limit, Offset: offset})
				}
			}
		case ScenarioStreamIndexOrder:
			for _, limit := range []uint32{20, 1000} {
				out = append(out, streamVariant{Scenario: scenario, Limit: limit, IndexOrder: true})
			}
		case ScenarioStreamFilter:
			out = append(out, streamVariant{Scenario: scenario, Limit: 100, Filter: true})
		case ScenarioStreamTraced:
			out = append(out, streamVariant{Scenario: scenario, Limit: 100, Trace: true})
		}
	}
	return out
}

func preloadStreamQuerySchema(shardNum, replicas int) func(context.Context, schema.Registry) error {
	return func(ctx context.Context, registry schema.Registry) error {
		updatedAt := timestamppb.New(time.Date(2021, 4, 15, 1, 30, 15, 10*int(time.Millisecond), time.UTC))
		group := &commonv1.Group{
			Metadata: &commonv1.Metadata{Name: sqbGroup},
			Catalog:  commonv1.Catalog_CATALOG_STREAM,
			ResourceOpts: &commonv1.ResourceOpts{
				ShardNum:        uint32(shardNum),
				Replicas:        uint32(replicas),
				SegmentInterval: &commonv1.IntervalRule{Unit: commonv1.IntervalRule_UNIT_DAY, Num: 1},
				Ttl:             &commonv1.IntervalRule{Unit: commonv1.IntervalRule_UNIT_DAY, Num: 30},
			},
			UpdatedAt: updatedAt,
		}
		if _, createErr := registry.CreateGroup(ctx, group); createErr != nil && !errors.Is(createErr, schema.ErrGRPCAlreadyExists) {
			return fmt.Errorf("create stream query benchmark group: %w", createErr)
		}
		stream := &databasev1.Stream{
			Metadata: &commonv1.Metadata{Name: sqbStream, Group: sqbGroup},
			TagFamilies: []*databasev1.TagFamilySpec{
				{Name: sqbFamilyData, Tags: []*databasev1.TagSpec{{Name: sqbTagPayload, Type: databasev1.TagType_TAG_TYPE_DATA_BINARY}}},
				{Name: sqbFamilyTags, Tags: []*databasev1.TagSpec{
					{Name: sqbTagRowKey, Type: databasev1.TagType_TAG_TYPE_STRING},
					{Name: sqbTagRegion, Type: databasev1.TagType_TAG_TYPE_STRING},
					{Name: sqbTagService, Type: databasev1.TagType_TAG_TYPE_STRING},
					{Name: sqbTagState, Type: databasev1.TagType_TAG_TYPE_INT},
					{Name: sqbTagDuration, Type: databasev1.TagType_TAG_TYPE_INT},
				}},
			},
			Entity:    &databasev1.Entity{TagNames: []string{sqbTagService}},
			UpdatedAt: updatedAt,
		}
		if _, createErr := registry.CreateStream(ctx, stream); createErr != nil && !errors.Is(createErr, schema.ErrGRPCAlreadyExists) {
			return fmt.Errorf("create stream query benchmark resource: %w", createErr)
		}
		for _, rule := range []struct{ name, tag string }{{sqbRuleRowKey, sqbTagRowKey}, {sqbRuleRegion, sqbTagRegion}} {
			indexRule := &databasev1.IndexRule{
				Metadata:  &commonv1.Metadata{Name: rule.name, Group: sqbGroup},
				Tags:      []string{rule.tag},
				Type:      databasev1.IndexRule_TYPE_INVERTED,
				UpdatedAt: updatedAt,
			}
			if _, createErr := registry.CreateIndexRule(ctx, indexRule); createErr != nil && !errors.Is(createErr, schema.ErrGRPCAlreadyExists) {
				return fmt.Errorf("create stream query benchmark index rule %s: %w", rule.name, createErr)
			}
		}
		binding := &databasev1.IndexRuleBinding{
			Metadata:  &commonv1.Metadata{Name: "bench-sw-index-rule-binding", Group: sqbGroup},
			Rules:     []string{sqbRuleRowKey, sqbRuleRegion},
			Subject:   &databasev1.Subject{Catalog: commonv1.Catalog_CATALOG_STREAM, Name: sqbStream},
			BeginAt:   timestamppb.New(time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)),
			ExpireAt:  timestamppb.New(time.Date(2121, 1, 1, 0, 0, 0, 0, time.UTC)),
			UpdatedAt: updatedAt,
		}
		if _, createErr := registry.CreateIndexRuleBinding(ctx, binding); createErr != nil && !errors.Is(createErr, schema.ErrGRPCAlreadyExists) {
			return fmt.Errorf("create stream query benchmark index rule binding: %w", createErr)
		}
		return nil
	}
}

// ---- deterministic data -------------------------------------------------------------------------------------------

func sqbElementID(idx int) string { return fmt.Sprintf("e%010d", idx) }

// sqbStoredElementID is the element ID a query returns: the hex of the stored hash of group|stream|id.
func sqbStoredElementID(idx int) string {
	return hex.EncodeToString(convert.Uint64ToBytes(convert.HashStr(sqbGroup + "|" + sqbStream + "|" + sqbElementID(idx))))
}

// sqbPerm is a bijection on [0, n) so the indexed row_key sorts differently from time and is unique per element.
func sqbPerm(idx, n int) int { return int((int64(idx)*sqbPermMul + sqbPermAdd) % int64(n)) }

func sqbRowKey(idx, n int) string { return fmt.Sprintf("k%09d", sqbPerm(idx, n)) }

// sqbInversePerm returns the element index whose row_key has the given rank.
func sqbInversePerm(rank, n int) int {
	inv := modInverse(sqbPermMul%int64(n), int64(n))
	shifted := ((int64(rank)-sqbPermAdd)%int64(n) + int64(n)) % int64(n)
	return int(shifted * inv % int64(n))
}

func modInverse(a, m int64) int64 {
	oldR, r := a, m
	oldS, s := int64(1), int64(0)
	for r != 0 {
		quotient := oldR / r
		oldR, r = r, oldR-quotient*r
		oldS, s = s, oldS-quotient*s
	}
	return (oldS%m + m) % m
}

func sqbRegionOf(idx int) int { return (idx / sqbServices) % sqbRegions }

func sqbService(idx int) string { return fmt.Sprintf("svc-%d", idx%sqbServices) }

func sqbPayload(idx, size int) []byte {
	payload := make([]byte, size)
	state := uint64(idx)*0x9E3779B97F4A7C15 + 0x1234567
	for pos := 0; pos < size; pos += 8 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		for shift := 0; shift < 8 && pos+shift < size; shift++ {
			payload[pos+shift] = byte(state >> (8 * shift))
		}
	}
	return payload
}

func sqbWriteRequest(idx, n, payloadBytes int, start time.Time, first bool) *streamv1.WriteRequest {
	req := &streamv1.WriteRequest{
		MessageId: uint64(idx) + 1,
		Element: &streamv1.ElementValue{
			ElementId: sqbElementID(idx),
			Timestamp: timestamppb.New(start.Add(time.Duration(idx) * time.Millisecond)),
			TagFamilies: []*modelv1.TagFamilyForWrite{
				{Tags: []*modelv1.TagValue{{Value: &modelv1.TagValue_BinaryData{BinaryData: sqbPayload(idx, payloadBytes)}}}},
				{Tags: []*modelv1.TagValue{
					strTagValue(sqbRowKey(idx, n)),
					strTagValue(fmt.Sprintf("r-%d", sqbRegionOf(idx))),
					strTagValue(sqbService(idx)),
					intTagValue(int64(idx % 7)),
					intTagValue(int64(1000 + idx%5000)),
				}},
			},
		},
	}
	if first {
		req.Metadata = &commonv1.Metadata{Name: sqbStream, Group: sqbGroup}
	}
	return req
}

func writeStreamQueryData(ctx context.Context, conn *grpc.ClientConn, cfg Config, n int, start time.Time) (writeSummary, error) {
	client := streamv1.NewStreamServiceClient(conn)
	writers := max(cfg.Writers, 1)
	per := (n + writers - 1) / writers
	group, groupCtx := errgroup.WithContext(ctx)
	began := time.Now()
	for writer := 0; writer < writers; writer++ {
		from, to := writer*per, min((writer+1)*per, n)
		if from >= to {
			continue
		}
		group.Go(func() error { return writeStreamRange(groupCtx, client, cfg, n, from, to, start) })
	}
	if waitErr := group.Wait(); waitErr != nil {
		return writeSummary{}, waitErr
	}
	elapsed := time.Since(began)
	return writeSummary{Rows: n, Duration: elapsed, RowsPerSec: float64(n) / elapsed.Seconds()}, nil
}

func writeStreamRange(ctx context.Context, client streamv1.StreamServiceClient, cfg Config, n, from, to int, start time.Time) error {
	stream, streamErr := client.Write(ctx)
	if streamErr != nil {
		return fmt.Errorf("open stream write: %w", streamErr)
	}
	recvErr := make(chan error, 1)
	go func() {
		acked := 0
		for {
			resp, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				if acked != to-from {
					recvErr <- fmt.Errorf("stream write acked %d of %d elements", acked, to-from)
					return
				}
				recvErr <- nil
				return
			}
			if err != nil {
				recvErr <- fmt.Errorf("receive stream write response: %w", err)
				return
			}
			if resp.GetStatus() != modelv1.Status_STATUS_SUCCEED.String() {
				recvErr <- fmt.Errorf("stream write failed message_id=%d status=%s", resp.GetMessageId(), resp.GetStatus())
				return
			}
			acked++
		}
	}()
	for idx := from; idx < to; idx++ {
		if sendErr := stream.Send(sqbWriteRequest(idx, n, cfg.StreamPayloadBytes, start, idx == from)); sendErr != nil {
			if errors.Is(sendErr, io.EOF) {
				break
			}
			return fmt.Errorf("send stream write: %w", sendErr)
		}
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return fmt.Errorf("close stream write: %w", closeErr)
	}
	return <-recvErr
}

// ---- queries ------------------------------------------------------------------------------------------------------

func sqbFullProjection() *modelv1.TagProjection {
	return &modelv1.TagProjection{TagFamilies: []*modelv1.TagProjection_TagFamily{
		{Name: sqbFamilyData, Tags: []string{sqbTagPayload}},
		{Name: sqbFamilyTags, Tags: []string{sqbTagRowKey, sqbTagRegion, sqbTagService, sqbTagState, sqbTagDuration}},
	}}
}

func sqbTimeRange(n int, start time.Time) *modelv1.TimeRange {
	return &modelv1.TimeRange{
		Begin: timestamppb.New(start.Add(-time.Hour)),
		End:   timestamppb.New(start.Add(time.Duration(n)*time.Millisecond + time.Hour)),
	}
}

func buildStreamQuery(v streamVariant, n int, start time.Time) *streamv1.QueryRequest {
	req := &streamv1.QueryRequest{
		Groups:     []string{sqbGroup},
		Name:       sqbStream,
		TimeRange:  sqbTimeRange(n, start),
		Limit:      v.Limit,
		Offset:     v.Offset,
		Trace:      v.Trace,
		Projection: sqbFullProjection(),
	}
	switch {
	case v.IndexOrder:
		req.OrderBy = &modelv1.QueryOrder{IndexRuleName: sqbRuleRowKey, Sort: modelv1.Sort_SORT_ASC}
	case v.Filter:
		req.Criteria = traceCondition(sqbTagRegion, modelv1.Condition_BINARY_OP_EQ, strTagValue(fmt.Sprintf("r-%d", sqbFilterRegion)))
		req.OrderBy = &modelv1.QueryOrder{Sort: modelv1.Sort_SORT_DESC}
	default:
		req.OrderBy = &modelv1.QueryOrder{Sort: modelv1.Sort_SORT_DESC}
	}
	return req
}

// expectedStreamIDs is the independent oracle: the ordered element IDs the window must return.
func expectedStreamIDs(v streamVariant, n int) []string {
	ids := make([]string, 0, v.Limit)
	switch {
	case v.IndexOrder:
		for rank := int(v.Offset); rank < n && len(ids) < int(v.Limit); rank++ {
			ids = append(ids, sqbStoredElementID(sqbInversePerm(rank, n)))
		}
	case v.Filter:
		skipped := 0
		for idx := n - 1; idx >= 0 && len(ids) < int(v.Limit); idx-- {
			if sqbRegionOf(idx) != sqbFilterRegion {
				continue
			}
			if skipped < int(v.Offset) {
				skipped++
				continue
			}
			ids = append(ids, sqbStoredElementID(idx))
		}
	default:
		for idx := n - 1 - int(v.Offset); idx >= 0 && len(ids) < int(v.Limit); idx-- {
			ids = append(ids, sqbStoredElementID(idx))
		}
	}
	return ids
}

func streamResponseIDs(resp *streamv1.QueryResponse) []string {
	ids := make([]string, len(resp.GetElements()))
	for idx, element := range resp.GetElements() {
		ids[idx] = element.GetElementId()
	}
	return ids
}

func hashStreamIDs(resp *streamv1.QueryResponse) uint64 {
	h := fnv.New64a()
	for _, element := range resp.GetElements() {
		_, _ = h.Write([]byte(element.GetElementId()))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}

func hashStreamContent(resp *streamv1.QueryResponse) uint64 {
	h := fnv.New64a()
	for _, element := range resp.GetElements() {
		body, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(element)
		if marshalErr != nil {
			continue
		}
		_, _ = h.Write(body)
	}
	return h.Sum64()
}

// waitStreamVisible waits until every written element is queryable exactly once, slice by slice.
func waitStreamVisible(ctx context.Context, conn *grpc.ClientConn, n int, start time.Time) error {
	client := streamv1.NewStreamServiceClient(conn)
	for first := 0; first < n; first += sqbVisibleSlice {
		want := min(sqbVisibleSlice, n-first)
		// The query time range is inclusive at both ends and needs millisecond precision.
		begin := start.Add(time.Duration(first) * time.Millisecond)
		end := start.Add(time.Duration(first+want-1) * time.Millisecond)
		deadline := time.Now().Add(3 * time.Minute)
		attempts := 0
		for {
			reqCtx, cancel := context.WithTimeout(ctx, queryTimeout(n))
			resp, queryErr := client.Query(reqCtx, &streamv1.QueryRequest{
				Groups:     []string{sqbGroup},
				Name:       sqbStream,
				TimeRange:  &modelv1.TimeRange{Begin: timestamppb.New(begin), End: timestamppb.New(end)},
				Limit:      uint32(want),
				OrderBy:    &modelv1.QueryOrder{Sort: modelv1.Sort_SORT_DESC},
				Projection: &modelv1.TagProjection{TagFamilies: []*modelv1.TagProjection_TagFamily{{Name: sqbFamilyTags, Tags: []string{sqbTagState}}}},
			})
			cancel()
			if queryErr == nil && len(resp.GetElements()) == want {
				break
			}
			attempts++
			if attempts%20 == 1 {
				got := -1
				if resp != nil {
					got = len(resp.GetElements())
				}
				fmt.Fprintf(os.Stderr, "[stream-bench] waiting for visibility slice=%d want=%d got=%d err=%v\n", first, want, got, queryErr)
			}
			if time.Now().After(deadline) {
				got := -1
				if resp != nil {
					got = len(resp.GetElements())
				}
				return fmt.Errorf("slice starting at %d never became fully visible: want=%d got=%d err=%v", first, want, got, queryErr)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	return nil
}

// waitStreamQuiescent waits until the in-process cluster stops burning CPU, so background flushes and merges of the
// freshly written data have finished and every run measures the same part layout. It reports how long it waited.
func waitStreamQuiescent(ctx context.Context, minWait, maxWait time.Duration) (time.Duration, error) {
	const (
		interval  = 2 * time.Second
		idleCores = 0.15
		needIdle  = 3
	)
	began := time.Now()
	idle := 0
	last := readCPUSeconds()
	for {
		select {
		case <-ctx.Done():
			return time.Since(began), ctx.Err()
		case <-time.After(interval):
		}
		now := readCPUSeconds()
		busy := (now - last) / interval.Seconds()
		last = now
		if busy < idleCores {
			idle++
		} else {
			idle = 0
		}
		elapsed := time.Since(began)
		if (idle >= needIdle && elapsed >= minWait) || elapsed >= maxWait {
			return elapsed, nil
		}
	}
}

// ---- measurement probes -------------------------------------------------------------------------------------------

// releaseStreamMemory settles the heap so a measured phase starts from a post-GC baseline.
func releaseStreamMemory() {
	runtime.GC()
	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(300 * time.Millisecond)
}

// memProbe polls heap-object bytes and RSS while a phase runs. The process hosts the liaison, the data nodes and the
// client, so the figures are process-wide; the baseline is taken after a forced GC.
type memProbe struct {
	stop           chan struct{}
	done           chan struct{}
	BaselineHeap   uint64
	BaselineRSS    uint64
	BaselineLive   uint64
	PeakLive       uint64
	PeakHeap       uint64
	PeakRSSPolled  uint64
	PeakRSSHWM     uint64
	hwmResetWorked bool
}

func startMemProbe() *memProbe {
	probe := &memProbe{stop: make(chan struct{}), done: make(chan struct{})}
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/gc/heap/live:bytes"}}
	metrics.Read(sample)
	probe.BaselineHeap = sample[0].Value.Uint64()
	probe.BaselineLive = sample[1].Value.Uint64()
	probe.PeakLive = probe.BaselineLive
	probe.BaselineRSS = readRSSBytes()
	probe.PeakHeap = probe.BaselineHeap
	probe.PeakRSSPolled = probe.BaselineRSS
	probe.hwmResetWorked = os.WriteFile("/proc/self/clear_refs", []byte("5"), 0o200) == nil
	go func() {
		defer close(probe.done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-probe.stop:
				return
			case <-ticker.C:
				metrics.Read(sample)
				if heap := sample[0].Value.Uint64(); heap > probe.PeakHeap {
					probe.PeakHeap = heap
				}
				if live := sample[1].Value.Uint64(); live > probe.PeakLive {
					probe.PeakLive = live
				}
				if rss := readRSSBytes(); rss > probe.PeakRSSPolled {
					probe.PeakRSSPolled = rss
				}
			}
		}
	}()
	return probe
}

func (p *memProbe) finish() {
	close(p.stop)
	<-p.done
	p.PeakRSSHWM = readVmHWMBytes()
}

func readVmHWMBytes() uint64 {
	body, readErr := os.ReadFile("/proc/self/status")
	if readErr != nil {
		return 0
	}
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "VmHWM:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kib, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return 0
		}
		return kib * 1024
	}
	return 0
}

// liaisonFrameRE selects the functions that only run in the liaison's query path: the public stream service, the
// distributed plan (including the goroutines and closures it spawns, whose names keep the method prefix) and the
// queue client that receives the data nodes' replies.
var liaisonFrameRE = regexp.MustCompile(`banyand/liaison/grpc\.|pkg/query/logical/stream\.[^ ]*[dD]istributed|banyand/queue/pub\.`)

func writeAllocsProfile() ([]byte, error) {
	var buf bytes.Buffer
	if writeErr := pprof.Lookup("allocs").WriteTo(&buf, 0); writeErr != nil {
		return nil, fmt.Errorf("write allocs profile: %w", writeErr)
	}
	return buf.Bytes(), nil
}

func sampleTypeIndex(p *profile.Profile, name string) int {
	for idx, st := range p.SampleType {
		if st.Type == name {
			return idx
		}
	}
	return -1
}

func sampleMatches(s *profile.Sample) bool {
	for _, loc := range s.Location {
		for _, line := range loc.Line {
			if line.Function != nil && liaisonFrameRE.MatchString(line.Function.Name) {
				return true
			}
		}
	}
	return false
}

// allocAttribution is the allocation delta of one phase, split into the liaison-attributed part and the total.
type allocAttribution struct {
	LiaisonBytes   float64
	LiaisonObjects float64
	TotalBytes     float64
	TotalObjects   float64
	Merged         *profile.Profile
}

func diffAllocProfiles(beforeRaw, afterRaw []byte) (allocAttribution, error) {
	before, parseErr := profile.ParseData(beforeRaw)
	if parseErr != nil {
		return allocAttribution{}, fmt.Errorf("parse before allocs profile: %w", parseErr)
	}
	after, parseErr := profile.ParseData(afterRaw)
	if parseErr != nil {
		return allocAttribution{}, fmt.Errorf("parse after allocs profile: %w", parseErr)
	}
	before.Scale(-1)
	merged, mergeErr := profile.Merge([]*profile.Profile{after, before})
	if mergeErr != nil {
		return allocAttribution{}, fmt.Errorf("merge allocs profiles: %w", mergeErr)
	}
	objIdx, spaceIdx := sampleTypeIndex(merged, "alloc_objects"), sampleTypeIndex(merged, "alloc_space")
	if objIdx < 0 || spaceIdx < 0 {
		return allocAttribution{}, errors.New("allocs profile has no alloc_objects/alloc_space sample types")
	}
	out := allocAttribution{Merged: merged}
	for _, s := range merged.Sample {
		out.TotalObjects += float64(s.Value[objIdx])
		out.TotalBytes += float64(s.Value[spaceIdx])
		if sampleMatches(s) {
			out.LiaisonObjects += float64(s.Value[objIdx])
			out.LiaisonBytes += float64(s.Value[spaceIdx])
		}
	}
	return out, nil
}

// cpuAttribution is the CPU time of the profiled phase, split into the liaison-attributed part and the total.
type cpuAttribution struct {
	LiaisonSeconds float64
	TotalSeconds   float64
}

func attributeCPU(raw []byte) (cpuAttribution, error) {
	parsed, parseErr := profile.ParseData(raw)
	if parseErr != nil {
		return cpuAttribution{}, fmt.Errorf("parse cpu profile: %w", parseErr)
	}
	idx := sampleTypeIndex(parsed, "cpu")
	if idx < 0 {
		return cpuAttribution{}, errors.New("cpu profile has no cpu sample type")
	}
	var out cpuAttribution
	for _, s := range parsed.Sample {
		out.TotalSeconds += float64(s.Value[idx]) / 1e9
		if sampleMatches(s) {
			out.LiaisonSeconds += float64(s.Value[idx]) / 1e9
		}
	}
	return out, nil
}

// streamPhase is the raw outcome of one concurrent query phase.
type streamPhase struct {
	Latencies []time.Duration
	Elapsed   time.Duration
}

// runStreamQueries issues iterations queries with the given worker count and checks every response against the
// expected row count and the ordered element-ID hash of the sample query.
func runStreamQueries(
	ctx context.Context, client streamv1.StreamServiceClient, req *streamv1.QueryRequest, n, iterations, workers int, wantRows int, wantHash uint64,
) (streamPhase, error) {
	jobs := make(chan struct{}, iterations)
	for iter := 0; iter < iterations; iter++ {
		jobs <- struct{}{}
	}
	close(jobs)
	var mu sync.Mutex
	latencies := make([]time.Duration, 0, iterations)
	group, groupCtx := errgroup.WithContext(ctx)
	began := time.Now()
	for worker := 0; worker < workers; worker++ {
		group.Go(func() error {
			for range jobs {
				queryCtx, cancel := context.WithTimeout(groupCtx, queryTimeout(n))
				started := time.Now()
				resp, queryErr := client.Query(queryCtx, proto.Clone(req).(*streamv1.QueryRequest))
				latency := time.Since(started)
				cancel()
				if queryErr != nil {
					return fmt.Errorf("stream query failed: %w", queryErr)
				}
				if len(resp.GetElements()) != wantRows {
					return fmt.Errorf("stream query returned %d rows, want %d", len(resp.GetElements()), wantRows)
				}
				if got := hashStreamIDs(resp); got != wantHash {
					return fmt.Errorf("stream query element-ID hash diverged across iterations: want=0x%x got=0x%x", wantHash, got)
				}
				mu.Lock()
				latencies = append(latencies, latency)
				mu.Unlock()
			}
			return nil
		})
	}
	if waitErr := group.Wait(); waitErr != nil {
		return streamPhase{}, waitErr
	}
	return streamPhase{Latencies: latencies, Elapsed: time.Since(began)}, nil
}

// liveHeapProbe is the outcome of the liaison retained-heap probe.
type liveHeapProbe struct {
	IdleBytes    float64
	MaxBytes     float64
	MeanBytes    float64
	MaxObjects   float64
	Samples      int
	QueriesSeen  int
	SampleBytes  []float64
	probeRateWas int
}

// liaisonLiveSample snapshots the heap profile and sums the in-use bytes and objects whose allocation stack runs
// through the liaison query path. The profile is as of the last completed GC; the query path allocates far more than
// the heap goal, so GCs complete every few milliseconds while a query runs and successive samples see fresh data.
func liaisonLiveSample() (bytesLive, objects float64, err error) {
	var buf bytes.Buffer
	if writeErr := pprof.Lookup("heap").WriteTo(&buf, 0); writeErr != nil {
		return 0, 0, fmt.Errorf("write heap profile: %w", writeErr)
	}
	parsed, parseErr := profile.ParseData(buf.Bytes())
	if parseErr != nil {
		return 0, 0, fmt.Errorf("parse heap profile: %w", parseErr)
	}
	spaceIdx, objIdx := sampleTypeIndex(parsed, "inuse_space"), sampleTypeIndex(parsed, "inuse_objects")
	if spaceIdx < 0 || objIdx < 0 {
		return 0, 0, errors.New("heap profile has no inuse sample types")
	}
	for _, sample := range parsed.Sample {
		if sampleMatches(sample) {
			bytesLive += float64(sample.Value[spaceIdx])
			objects += float64(sample.Value[objIdx])
		}
	}
	return bytesLive, objects, nil
}

// probeLiaisonLiveHeap runs serial queries while a sampler repeatedly snapshots the heap profile and measures how much
// heap the liaison's query path still holds. The data nodes share the process, so only samples whose stack runs through the
// liaison are counted; this isolates the memory the liaison retains (decoded frames, aligned copies, elements,
// the response) from the data nodes' scan buffers. The maximum over the samples approximates the per-query peak.
func probeLiaisonLiveHeap(
	ctx context.Context, client streamv1.StreamServiceClient, req *streamv1.QueryRequest, n, queries int, wantRows int, wantHash uint64,
) (liveHeapProbe, error) {
	out := liveHeapProbe{probeRateWas: runtime.MemProfileRate}
	runtime.MemProfileRate = 4096
	defer func() { runtime.MemProfileRate = out.probeRateWas }()
	runtime.GC()
	runtime.GC()
	idle, _, idleErr := liaisonLiveSample()
	if idleErr != nil {
		return out, idleErr
	}
	out.IdleBytes = idle
	var mu sync.Mutex
	var sampleErr error
	for query := 0; query < queries; query++ {
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-stop:
					return
				default:
				}
				liveBytes, liveObjs, err := liaisonLiveSample()
				mu.Lock()
				if err != nil {
					sampleErr = err
				} else {
					out.SampleBytes = append(out.SampleBytes, liveBytes)
					out.Samples++
					out.MeanBytes += liveBytes
					out.MaxBytes = max(out.MaxBytes, liveBytes)
					out.MaxObjects = max(out.MaxObjects, liveObjs)
				}
				mu.Unlock()
			}
		}()
		_, queryErr := runStreamQueries(ctx, client, req, n, 1, 1, wantRows, wantHash)
		close(stop)
		<-done
		if queryErr != nil {
			return out, queryErr
		}
		out.QueriesSeen++
	}
	if sampleErr != nil {
		return out, sampleErr
	}
	if out.Samples > 0 {
		out.MeanBytes /= float64(out.Samples)
	}
	return out, nil
}
