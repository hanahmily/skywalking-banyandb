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

package p6

import (
	"os"
	"strconv"
	"strings"
	"testing"

	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	"github.com/apache/skywalking-banyandb/pkg/test"
	"github.com/apache/skywalking-banyandb/pkg/test/setup"
)

func startCluster(t *testing.T) string {
	t.Helper()
	t.Setenv("POD_NAME", "localhost")
	root := t.TempDir()
	config := setup.PropertyClusterConfig(setup.NewDiscoveryFileWriter(root))
	if os.Getenv("P6_DISTRIBUTED") == "1" {
		for index := 0; index < 2; index++ {
			addr, _, _, stop := setup.DataNodeWithAddrAndDir(config, "--measure-vectorized-query-memory-mib=256")
			t.Logf("P6 worker%d=%s", index, addr)
			t.Cleanup(stop)
		}
		addr, stop := setup.LiaisonNode(config, "--measure-vectorized-query-memory-mib=256")
		t.Cleanup(stop)
		return addr
	}
	ports, err := test.AllocateFreePorts(5)
	if err != nil {
		t.Fatal(err)
	}
	addr, _, stop := setup.EmptyClosableStandalone(config, root, ports, "--measure-vectorized-query-memory-mib=256")
	t.Cleanup(stop)
	return addr
}

func verifyWorkerRows(t *testing.T, trace *commonv1.Trace, want int) map[string]int {
	t.Helper()
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
						t.Fatal(err)
					}
					workers[worker] += rows
				}
			}
		}
		for _, child := range span.Children {
			walk(child, worker)
		}
	}
	for _, span := range trace.GetSpans() {
		walk(span, "")
	}
	total := 0
	for _, rows := range workers {
		total += rows
		if rows == 0 {
			t.Fatalf("worker did not contribute fixture rows: %v", workers)
		}
	}
	if len(workers) != 2 || total != want {
		t.Fatalf("worker rows%v, want%d across2workers", workers, want)
	}
	return workers
}
