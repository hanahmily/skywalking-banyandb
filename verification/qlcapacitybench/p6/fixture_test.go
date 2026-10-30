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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	bydbqlv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/bydbql/v1"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	measurev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/measure/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	testflags "github.com/apache/skywalking-banyandb/pkg/test/flags"
	"github.com/apache/skywalking-banyandb/test/cases/measure/qlgen"
)

const exportQL = "SELECT service_id, COUNT(DISTINCT entity_id) AS distinct_count FROM p6bench.m " +
	"WHERE time >= ? AND time < ? AND value::FIELD > 0 GROUP BY service_id ORDER BY distinct_count DESC, service_id ASC"

func tag(s string) *modelv1.TagValue {
	return &modelv1.TagValue{Value: &modelv1.TagValue_Str{Str: &modelv1.Str{Value: s}}}
}

func field(n int64) *modelv1.FieldValue {
	return &modelv1.FieldValue{Value: &modelv1.FieldValue_Int{Int: &modelv1.Int{Value: n}}}
}
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func request(base time.Time, service string) *bydbqlv1.QueryRequest {
	q := exportQL
	params := []*modelv1.TagValue{
		{Value: &modelv1.TagValue_Timestamp{Timestamp: timestamppb.New(base)}},
		{Value: &modelv1.TagValue_Timestamp{Timestamp: timestamppb.New(base.Add(time.Hour))}},
	}
	if service != "" {
		q = "SELECT service_id, COUNT(DISTINCT entity_id) AS distinct_count FROM p6bench.m " +
			"WHERE time >= ? AND time < ? AND service_id = ? AND value::FIELD > 0 GROUP BY service_id ORDER BY distinct_count DESC, service_id ASC"
		params = append(params, tag(service))
	}
	return &bydbqlv1.QueryRequest{Query: q, Params: params}
}

func setupSchema(ctx context.Context, conn *grpc.ClientConn) error {
	group := qlgen.P1FixtureGroup()
	group.Metadata.Name = "p6bench"
	if _, err := databasev1.NewGroupRegistryServiceClient(conn).Create(ctx, &databasev1.GroupRegistryServiceCreateRequest{Group: group}); err != nil {
		return err
	}
	for _, rule := range qlgen.P1IndexRules() {
		rule.Metadata.Group = "p6bench"
		if _, err := databasev1.NewIndexRuleRegistryServiceClient(conn).Create(ctx, &databasev1.IndexRuleRegistryServiceCreateRequest{IndexRule: rule}); err != nil {
			return err
		}
	}
	binding := qlgen.P1IndexBinding()
	binding.Metadata.Group = "p6bench"
	binding.Subject.Name = "m"
	if _, err := databasev1.NewIndexRuleBindingRegistryServiceClient(conn).Create(ctx,
		&databasev1.IndexRuleBindingRegistryServiceCreateRequest{IndexRuleBinding: binding}); err != nil {
		return err
	}
	schema := qlgen.P1FixtureSchema()
	schema.Metadata = &commonv1.Metadata{Name: "m", Group: "p6bench"}
	_, err := databasev1.NewMeasureRegistryServiceClient(conn).Create(ctx, &databasev1.MeasureRegistryServiceCreateRequest{Measure: schema})
	return err
}

func writeFixture(ctx context.Context, conn *grpc.ClientConn, base time.Time, groups int) (int, string, error) {
	writer := measurev1.NewMeasureServiceClient(conn)
	hash := sha256.New()
	rows := 0
	// Bounded streams keep acknowledgement backpressure independent of fixture size.
	for start := 0; start < groups; start += 250 {
		stream, err := writer.Write(ctx)
		if err != nil {
			return rows, "", err
		}
		sent := 0
		for g := start; g < min(groups, start+250); g++ {
			for entity := 0; entity < 1+g%3; entity++ {
				for stamp := 0; stamp < 2; stamp++ {
					req := &measurev1.WriteRequest{
						MessageId: uint64(rows + 1),
						DataPointSpec: &measurev1.DataPointSpec{TagFamilySpec: []*measurev1.TagFamilySpec{
							{Name: "storage-only", TagNames: []string{"entity_id"}}, {Name: "searchable", TagNames: []string{"service_id"}},
						}, FieldNames: []string{"value", "summation", "count"}},
						DataPoint: &measurev1.DataPointValue{
							Timestamp: timestamppb.New(base.Add(time.Duration(stamp) * time.Second)),
							TagFamilies: []*modelv1.TagFamilyForWrite{
								{Tags: []*modelv1.TagValue{tag(fmt.Sprintf("e%06d_%d", g, entity))}},
								{Tags: []*modelv1.TagValue{tag(fmt.Sprintf("s%06d", g))}},
							}, Fields: []*modelv1.FieldValue{field(int64(stamp + 1)), field(int64(stamp + 1)), field(1)},
						},
					}
					if sent == 0 {
						req.Metadata = &commonv1.Metadata{Name: "m", Group: "p6bench"}
					}
					raw, marshalErr := proto.MarshalOptions{Deterministic: true}.Marshal(req)
					if marshalErr != nil {
						return rows, "", marshalErr
					}
					hash.Write(raw)
					if err = stream.Send(req); err != nil {
						return rows, "", err
					}
					sent++
					rows++
				}
			}
		}
		if err = stream.CloseSend(); err != nil {
			return rows, "", err
		}
		received := 0
		for {
			ack, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}
			if recvErr != nil {
				return rows, "", recvErr
			}
			if ack.GetStatus() != modelv1.Status_STATUS_SUCCEED.String() {
				return rows, "", fmt.Errorf("write status %s", ack.GetStatus())
			}
			received++
		}
		if received != sent {
			return rows, "", fmt.Errorf("acknowledgements %d != %d", received, sent)
		}
	}
	return rows, hex.EncodeToString(hash.Sum(nil)), nil
}

func TestExportRPC(t *testing.T) {
	gomega.RegisterTestingT(t)
	testflags.LogLevel = "error"
	if err := logger.Init(logger.Logging{Env: "dev", Level: "error"}); err != nil {
		t.Fatal(err)
	}
	groups, err := strconv.Atoi(os.Getenv("P6_ROWS"))
	if err != nil || groups < 1 {
		t.Fatal("invalid P6_ROWS")
	}
	base, err := time.Parse(time.RFC3339, os.Getenv("P6_BASE_TIME"))
	if err != nil {
		t.Fatal(err)
	}
	addr := startCluster(t)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	if err = setupSchema(ctx, conn); err != nil {
		t.Fatal(err)
	}
	// Wait for schema routing before beginning the one fixture write.
	client := bydbqlv1.NewBydbQLServiceClient(conn)
	for deadline := time.Now().Add(time.Minute); ; {
		_, err = client.Query(ctx, request(base, "s000000"))
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows, fixtureHash, err := writeFixture(ctx, conn, base, groups)
	if err != nil {
		t.Fatal(err)
	}
	emit(t, map[string]any{"phase": "fixture", "groups": groups, "input_rows": rows, "fixture_sha256": fixtureHash, "base_time": base, "client_max_receive_bytes": 4194304})
	for _, g := range []int{0, groups / 2, groups - 1} {
		for deadline := time.Now().Add(time.Minute); ; {
			response, queryErr := client.Query(ctx, request(base, fmt.Sprintf("s%06d", g)))
			if queryErr == nil {
				queryErr = check(response, []answer{{fmt.Sprintf("s%06d", g), int64(1 + g%3)}})
			}
			if queryErr == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("visibility group%d: %v", g, queryErr)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	runExport(t, ctx, client, base, groups)
}
