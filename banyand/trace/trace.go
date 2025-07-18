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

// Package trace implements a trace-based storage which consists of trace data.
// Traces are composed of spans and support querying by trace ID and various tags.
package trace

import (
	"context"
	"sync/atomic"
	"time"

	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	"github.com/apache/skywalking-banyandb/banyand/protector"
	"github.com/apache/skywalking-banyandb/banyand/queue"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/query/model"
	"github.com/apache/skywalking-banyandb/pkg/run"
	"github.com/apache/skywalking-banyandb/pkg/schema"
	"github.com/apache/skywalking-banyandb/pkg/timestamp"
)

const (
	defaultFlushTimeout              = time.Second
	defaultElementIndexFlushTimeout = time.Second
)

type option struct {
	mergePolicy              *mergePolicy
	protector                protector.Memory
	tire2Client              queue.Client
	seriesCacheMaxSize       run.Bytes
	flushTimeout             time.Duration
	elementIndexFlushTimeout time.Duration
}

// Service allows inspecting the trace data.
type Service interface {
	run.PreRunner
	run.Config
	run.Service
	Query
}

// Query allows retrieving traces.
type Query interface {
	LoadGroup(name string) (schema.Group, bool)
	Trace(metadata *commonv1.Metadata) (Trace, error)
	GetRemovalSegmentsTimeRange(group string) *timestamp.TimeRange
}

// Trace allows inspecting trace details.
type Trace interface {
	GetSchema() *databasev1.Trace
	GetIndexRules() []*databasev1.IndexRule
	Query(ctx context.Context, opts model.TraceQueryOptions) (model.TraceQueryResult, error)
}

type indexSchema struct {
	tagMap            map[string]*databasev1.TraceTagSpec
	indexRuleLocators interface{} // Will be properly typed when trace partition support is added
	indexRules        []*databasev1.IndexRule
}

func (i *indexSchema) parse(schema *databasev1.Trace) {
	// Note: This will need proper implementation when trace partition support is added
	// For now, we'll just parse the tag map
	i.tagMap = make(map[string]*databasev1.TraceTagSpec)
	for _, tag := range schema.GetTags() {
		i.tagMap[tag.GetName()] = tag
	}
}

type trace struct {
	pm         protector.Memory
	indexSchema atomic.Value
	tsdb       atomic.Value
	l          *logger.Logger
	schema     *databasev1.Trace
	schemaRepo *schemaRepo
	name       string
	group      string
}

type traceSpec struct {
	schema *databasev1.Trace
}

func (t *trace) GetSchema() *databasev1.Trace {
	return t.schema
}

func (t *trace) GetIndexRules() []*databasev1.IndexRule {
	if is := t.indexSchema.Load(); is != nil {
		return is.(*indexSchema).indexRules
	}
	return nil
}

func (t *trace) Query(ctx context.Context, opts model.TraceQueryOptions) (model.TraceQueryResult, error) {
	// TODO: Implement trace query logic
	// This is a placeholder implementation
	return nil, nil
}

// mergePolicy defines the merge policy for trace data
type mergePolicy struct {
	// TODO: Define merge policy specific to traces
}