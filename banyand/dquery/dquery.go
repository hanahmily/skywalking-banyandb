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

// Package dquery implement the distributed query.
package dquery

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/multierr"

	"github.com/apache/skywalking-banyandb/api/common"
	"github.com/apache/skywalking-banyandb/api/data"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	modelv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v1"
	"github.com/apache/skywalking-banyandb/banyand/measure"
	"github.com/apache/skywalking-banyandb/banyand/metadata"
	"github.com/apache/skywalking-banyandb/banyand/observability"
	"github.com/apache/skywalking-banyandb/banyand/queue"
	"github.com/apache/skywalking-banyandb/banyand/stream"
	"github.com/apache/skywalking-banyandb/pkg/bus"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/query/executor"
	"github.com/apache/skywalking-banyandb/pkg/run"
)

const (
	moduleName   = "distributed-query"
	hotStageName = "hot"
)

var _ run.Service = (*queryService)(nil)

// Service is the interface for distributed query service.
type Service interface {
	run.Unit
}

var _ Service = (*queryService)(nil)

type queryService struct {
	metaService          metadata.Repo
	pipeline             queue.Server
	omr                  observability.MetricsRegistry
	log                  *logger.Logger
	sqp                  *streamQueryProcessor
	mqp                  *measureQueryProcessor
	tqp                  *topNQueryProcessor
	closer               *run.Closer
	nodeID               string
	hotStageNodeSelector string
	slowQuery            time.Duration
}

// NewService return a new query service.
func NewService(metaService metadata.Repo, pipeline queue.Server, broadcaster bus.Broadcaster, omr observability.MetricsRegistry,
	streamSchemaSVC stream.Service, measureSchemaSVC measure.Service,
) (Service, error) {
	svc := &queryService{
		metaService: metaService,
		closer:      run.NewCloser(1),
		pipeline:    pipeline,
		omr:         omr,
	}
	svc.sqp = &streamQueryProcessor{
		queryService:  svc,
		streamService: streamSchemaSVC,
		broadcaster:   broadcaster,
	}
	svc.mqp = &measureQueryProcessor{
		queryService:   svc,
		measureService: measureSchemaSVC,
		broadcaster:    broadcaster,
	}
	svc.tqp = &topNQueryProcessor{
		queryService: svc,
		broadcaster:  broadcaster,
	}
	return svc, nil
}

func (q *queryService) Name() string {
	return moduleName
}

func (q *queryService) FlagSet() *run.FlagSet {
	fs := run.NewFlagSet("distributed-query")
	fs.DurationVar(&q.slowQuery, "dst-slow-query", 5*time.Second, "distributed slow query threshold, 0 means no slow query log")
	return fs
}

func (q *queryService) Validate() error {
	return nil
}

func (q *queryService) PreRun(ctx context.Context) error {
	val := ctx.Value(common.ContextNodeKey)
	if val == nil {
		return errors.New("node id is empty")
	}
	node := val.(common.Node)
	q.nodeID = node.NodeID
	val = ctx.Value(common.ContextNodeSelectorKey)
	if val != nil {
		q.hotStageNodeSelector = val.(string)
	}

	q.log = logger.GetLogger(moduleName)
	q.tqp.measureService = q.mqp.measureService
	return multierr.Combine(
		q.pipeline.Subscribe(data.TopicStreamQuery, q.sqp),
		q.pipeline.Subscribe(data.TopicMeasureQuery, q.mqp),
		q.pipeline.Subscribe(data.TopicTopNQuery, q.tqp),
	)
}

func (q *queryService) GracefulStop() {
	q.closer.Done()
	q.closer.CloseThenWait()
}

func (q *queryService) Serve() run.StopNotify {
	return q.closer.CloseNotify()
}

func (q *queryService) parseNodeSelector(stages []string, resource *commonv1.ResourceOpts) ([]string, bool) {
	if len(stages) == 0 {
		stages = resource.DefaultStages
	}
	if len(stages) == 0 {
		return nil, false
	}

	var nodeSelectors []string
	for _, sn := range stages {
		for _, stage := range resource.Stages {
			if strings.EqualFold(sn, stage.Name) {
				ns := stage.NodeSelector
				ns = strings.TrimSpace(ns)
				if ns == "" {
					continue
				}
				nodeSelectors = append(nodeSelectors, ns)
				break
			}
		}
		if strings.EqualFold(sn, hotStageName) && q.hotStageNodeSelector != "" {
			nodeSelectors = append(nodeSelectors, q.hotStageNodeSelector)
		}
	}
	if len(nodeSelectors) == 0 {
		return nil, false
	}
	return nodeSelectors, true
}

var _ executor.DistributedExecutionContext = (*distributedContext)(nil)

type distributedContext struct {
	bus.Broadcaster
	timeRange     *modelv1.TimeRange
	nodeSelectors map[string][]string
}

func (dc *distributedContext) TimeRange() *modelv1.TimeRange {
	return dc.timeRange
}

func (dc *distributedContext) NodeSelectors() map[string][]string {
	return dc.nodeSelectors
}
