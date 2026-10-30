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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	"github.com/apache/skywalking-banyandb/banyand/backup/lifecycle"
	"github.com/apache/skywalking-banyandb/banyand/metadata/schema"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/test/setup"
	"github.com/apache/skywalking-banyandb/test/cases/measure/qlgen"
)

// TestP2Stages qualifies real tier migration before any P2 timings.
func TestP2Stages(t *testing.T) {
	if os.Getenv("P2_STAGE_VERIFY") != "1" {
		t.Skip("set P2_STAGE_VERIFY=1 for the real lifecycle checkpoint")
	}
	gomega.RegisterTestingT(t)
	if err := logger.Init(logger.Logging{Env: "dev", Level: "error"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POD_NAME", "localhost")
	root := t.TempDir()
	discovery := setup.NewDiscoveryFileWriter(root)
	config := setup.PropertyClusterConfig(discovery)
	type node struct{ addr, dir string }
	nodes := map[string]node{}
	for _, tier := range []string{"hot", "warm", "cold"} {
		flags := []string{"--node-labels", "type=" + tier, "--measure-flush-timeout", "0s"}
		if tier != "hot" {
			flags = append(flags, "--has-meta-role=false")
		}
		addr, dir, _, stop := setup.DataNodeWithAddrAndDir(config, flags...)
		t.Cleanup(stop)
		nodes[tier] = node{addr, dir}
	}
	setup.PreloadSchemaViaProperty(config, func(ctx context.Context, registry schema.Registry) error {
		group := qlgen.P1FixtureGroup()
		group.ResourceOpts.Ttl.Num = 3
		for i, tier := range []string{"warm", "cold"} {
			ttl := uint32(7)
			if i == 1 {
				ttl = 30
			}
			group.ResourceOpts.Stages = append(group.ResourceOpts.Stages, &commonv1.LifecycleStage{
				Name: tier, ShardNum: 2,
				SegmentInterval: &commonv1.IntervalRule{Unit: commonv1.IntervalRule_UNIT_DAY, Num: 1},
				Ttl:             &commonv1.IntervalRule{Unit: commonv1.IntervalRule_UNIT_DAY, Num: ttl}, NodeSelector: "type=" + tier,
			})
		}
		if _, err := registry.CreateGroup(ctx, group); err != nil {
			return err
		}
		if _, err := registry.CreateMeasure(ctx, qlgen.P1FixtureSchema()); err != nil {
			return err
		}
		for _, rule := range qlgen.P1IndexRules() {
			if _, err := registry.CreateIndexRule(ctx, rule); err != nil {
				return err
			}
		}
		_, err := registry.CreateIndexRuleBinding(ctx, qlgen.P1IndexBinding())
		return err
	})
	config.AddLoadedKinds(schema.KindMeasure)
	addr, stop := setup.LiaisonNode(config, "--data-node-selector", "type=hot")
	t.Cleanup(stop)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	base := time.Now().UTC().Truncate(24 * time.Hour)
	p2WriteStageRows(t, ctx, conn, base)
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	witnesses := []map[string]string{}
	query := func(expression, stages string, want string) {
		t.Helper()
		text := fmt.Sprintf("SELECT %s AS n FROM MEASURE activity IN qlperf ON (%s) STAGES TIME BETWEEN '%s' AND '%s'",
			expression, stages, base.Add(-13*24*time.Hour).Format(time.RFC3339), base.Add(time.Hour).Format(time.RFC3339))
		var last error
		for deadline := time.Now().Add(time.Minute); ; {
			response, queryErr := client.Query(ctx, &bydbqlv1.QueryRequest{Query: text})
			last = queryErr
			if status.Code(queryErr) == codes.InvalidArgument {
				t.Fatalf("invalid stage query: %v", queryErr)
			}
			if queryErr == nil {
				last = checkResult(response, want)
			}
			if last == nil {
				t.Logf("stage witness %s %s: %s", expression, stages, want)
				witnesses = append(witnesses, map[string]string{"expression": expression, "stages": stages, "expected": want, "query": text})
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("stage %s: %v", stages, last)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	query("COUNT(entity_id)", "hot", ":6")
	for _, tier := range []string{"hot", "warm"} {
		n := nodes[tier]
		command := lifecycle.NewCommand()
		command.SetArgs([]string{
			"--grpc-addr", n.addr, "--node-labels", "type=" + tier,
			"--measure-root-path", n.dir, "--stream-root-path", n.dir, "--trace-root-path", n.dir,
			"--progress-file", filepath.Join(root, tier+"-progress.json"), "--report-dir", filepath.Join(root, tier+"-report"),
			"--node-discovery-mode=file", "--node-discovery-file-path", discovery.Path(),
		})
		if err = command.Execute(); err != nil {
			t.Fatalf("%s lifecycle: %v", tier, err)
		}
		if tier == "hot" {
			query("COUNT(entity_id)", "warm", ":4")
		}
	}
	for _, tier := range []string{"hot", "warm", "cold"} {
		query("COUNT(DISTINCT entity_id)", tier, ":2")
	}
	query("COUNT(DISTINCT entity_id)", "hot,warm", ":3")
	query("COUNT(DISTINCT entity_id)", "hot,warm,cold", ":4")
	query("SUM(value::FIELD)", "hot,warm,cold", ":6")
	if output := os.Getenv("P2_STAGE_OUTPUT"); output != "" {
		data, marshalErr := json.MarshalIndent(map[string]any{"status": "passed", "rows": 6, "base_time": base, "witnesses": witnesses}, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err = os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func p2WriteStageRows(t *testing.T, ctx context.Context, conn *grpc.ClientConn, base time.Time) {
	t.Helper()
	fixtures := qlgen.P1FixtureWrites(base)
	for index, days := range []int{1, 1, 5, 5, 12, 12} {
		stream, writeErr := measurev1.NewMeasureServiceClient(conn).Write(ctx)
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		req := fixtures[0]
		req = proto.Clone(req).(*measurev1.WriteRequest)
		req.DataPoint.TagFamilies[0].Tags[0] = &modelv1.TagValue{Value: &modelv1.TagValue_Str{Str: &modelv1.Str{Value: []string{"a", "b", "b", "c", "c", "d"}[index]}}}
		req.DataPoint.Timestamp = timestamppb.New(base.Add(-time.Duration(days) * 24 * time.Hour).Add(time.Duration(index) * time.Second))
		if writeErr = stream.Send(req); writeErr != nil {
			t.Fatal(writeErr)
		}
		if writeErr = stream.CloseSend(); writeErr != nil {
			t.Fatal(writeErr)
		}
		count := 0
		for {
			ack, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				t.Fatal(recvErr)
			}
			if ack.Status != modelv1.Status_STATUS_SUCCEED.String() {
				t.Fatalf("write rejected: %s", ack.Status)
			}
			count++
		}
		if count != 1 {
			t.Fatalf("write acknowledgements=%d", count)
		}
	}
}
