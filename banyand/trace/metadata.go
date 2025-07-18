// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package trace

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/pkg/errors"

	"github.com/apache/skywalking-banyandb/api/common"
	commonv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/common/v1"
	databasev1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/database/v1"
	"github.com/apache/skywalking-banyandb/api/validate"
	"github.com/apache/skywalking-banyandb/banyand/internal/storage"
	"github.com/apache/skywalking-banyandb/banyand/liaison/grpc"
	"github.com/apache/skywalking-banyandb/banyand/metadata"
	"github.com/apache/skywalking-banyandb/banyand/metadata/schema"
	"github.com/apache/skywalking-banyandb/banyand/observability"
	"github.com/apache/skywalking-banyandb/banyand/protector"
	"github.com/apache/skywalking-banyandb/pkg/logger"
	"github.com/apache/skywalking-banyandb/pkg/meter"
	resourceSchema "github.com/apache/skywalking-banyandb/pkg/schema"
	"github.com/apache/skywalking-banyandb/pkg/timestamp"
)

var metadataScope = meter.NewScope("trace").SubScope("metadata")

// SchemaService allows querying schema information.
type SchemaService interface {
	Query
	Close()
}

type schemaRepo struct {
	resourceSchema.Repository
	l        *logger.Logger
	metadata metadata.Repo
	path     string
}

func newSchemaRepo(path string, svc *standalone, nodeLabels map[string]string) schemaRepo {
	sr := schemaRepo{
		l:        svc.l,
		path:     path,
		metadata: svc.metadata,
		Repository: resourceSchema.NewRepository(
			svc.metadata,
			svc.l,
			newSupplier(path, svc, nodeLabels),
			resourceSchema.NewMetrics(svc.omr.With(metadataScope)),
		),
	}
	sr.start()
	return sr
}

func newLiaisonSchemaRepo(path string, svc *liaison, traceDataNodeRegistry grpc.NodeRegistry) schemaRepo {
	sr := schemaRepo{
		l:        svc.l,
		path:     path,
		metadata: svc.metadata,
		Repository: resourceSchema.NewRepository(
			svc.metadata,
			svc.l,
			newQueueSupplier(path, svc, traceDataNodeRegistry),
			resourceSchema.NewMetrics(svc.omr.With(metadataScope)),
		),
	}
	sr.start()
	return sr
}

func (sr *schemaRepo) start() {
	sr.l.Info().Str("path", sr.path).Msg("starting trace metadata repository")
}

func (sr *schemaRepo) LoadGroup(name string) (resourceSchema.Group, bool) {
	return sr.Repository.LoadGroup(name)
}

func (sr *schemaRepo) Trace(metadata *commonv1.Metadata) (Trace, error) {
	sm, err := sr.Repository.LoadResource(metadata)
	if err != nil {
		return nil, err
	}
	
	return sm.(Trace), nil
}

func (sr *schemaRepo) GetRemovalSegmentsTimeRange(group string) *timestamp.TimeRange {
	g, ok := sr.LoadGroup(group)
	if !ok {
		return nil
	}
	return g.GetRemovalSegmentsTimeRange()
}

// supplier is the supplier for standalone service
type supplier struct {
	metadata   metadata.Repo
	omr        observability.MetricsRegistry
	pm         protector.Memory
	l          *logger.Logger
	schemaRepo *schemaRepo
	nodeLabels map[string]string
	path       string
	option     option
}

func newSupplier(path string, svc *standalone, nodeLabels map[string]string) *supplier {
	return &supplier{
		metadata:   svc.metadata,
		omr:        svc.omr,
		pm:         svc.pm,
		l:          svc.l,
		nodeLabels: nodeLabels,
		path:       path,
		option:     svc.option,
	}
}

func (s *supplier) OpenResource(spec resourceSchema.Resource) (resourceSchema.IndexListener, error) {
	traceSpec, ok := spec.(*traceSpec)
	if !ok {
		return nil, errors.New("invalid resource type")
	}

	db, err := storage.OpenTSDB(
		context.WithValue(context.Background(), logger.ContextKey, s.l),
		storage.TSDBOpts{
			Location:       path.Join(s.path, traceSpec.schema.GetMetadata().GetGroup(), traceSpec.schema.GetMetadata().GetName()),
			TSTableOptions: storage.DefaultTSTableOptions(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("cannot open tsdb %s: %w", traceSpec.schema.GetMetadata().GetName(), err)
	}

	t := &trace{
		pm:         s.pm,
		l:          s.l,
		schema:     traceSpec.schema,
		schemaRepo: s.schemaRepo,
		name:       traceSpec.schema.GetMetadata().GetName(),
		group:      traceSpec.schema.GetMetadata().GetGroup(),
	}
	t.tsdb.Store(db)

	// Parse index schema
	is := &indexSchema{
		indexRules: schema.IndexRules(traceSpec.schema),
	}
	is.parse(traceSpec.schema)
	t.indexSchema.Store(is)

	return t, nil
}

func (s *supplier) ResourceSchema(md *commonv1.Metadata) (resourceSchema.ResourceSchema, error) {
	tr, err := s.metadata.TraceRegistry().GetTrace(context.TODO(), md)
	if err != nil {
		return nil, err
	}
	if err := validate.Trace(tr); err != nil {
		return nil, err
	}
	return &traceSpec{schema: tr}, nil
}

// queueSupplier is the supplier for liaison service
type queueSupplier struct {
	metadata               metadata.Repo
	omr                    observability.MetricsRegistry
	pm                     protector.Memory
	traceDataNodeRegistry  grpc.NodeRegistry
	l                      *logger.Logger
	schemaRepo             *schemaRepo
	path                   string
	option                 option
}

func newQueueSupplier(path string, svc *liaison, traceDataNodeRegistry grpc.NodeRegistry) *queueSupplier {
	return &queueSupplier{
		metadata:              svc.metadata,
		omr:                   svc.omr,
		pm:                    svc.pm,
		traceDataNodeRegistry: traceDataNodeRegistry,
		l:                     svc.l,
		path:                  path,
		option:                svc.option,
	}
}

func (qs *queueSupplier) OpenResource(spec resourceSchema.Resource) (resourceSchema.IndexListener, error) {
	traceSpec, ok := spec.(*traceSpec)
	if !ok {
		return nil, errors.New("invalid resource type")
	}

	t := &trace{
		pm:         qs.pm,
		l:          qs.l,
		schema:     traceSpec.schema,
		schemaRepo: qs.schemaRepo,
		name:       traceSpec.schema.GetMetadata().GetName(),
		group:      traceSpec.schema.GetMetadata().GetGroup(),
	}

	// Parse index schema
	is := &indexSchema{
		indexRules: schema.IndexRules(traceSpec.schema),
	}
	is.parse(traceSpec.schema)
	t.indexSchema.Store(is)

	return t, nil
}

func (qs *queueSupplier) ResourceSchema(md *commonv1.Metadata) (resourceSchema.ResourceSchema, error) {
	tr, err := qs.metadata.TraceRegistry().GetTrace(context.TODO(), md)
	if err != nil {
		return nil, err
	}
	if err := validate.Trace(tr); err != nil {
		return nil, err
	}
	return &traceSpec{schema: tr}, nil
}

// Spec returns the databasev1.Trace
func (ts *traceSpec) Spec() any {
	return ts.schema
}

// Schema returns the common.Metadata
func (ts *traceSpec) Schema() *commonv1.Metadata {
	return ts.schema.GetMetadata()
}

// IndexRuleBinding returns the IndexRuleBinding
func (ts *traceSpec) IndexRuleBinding() *databasev1.IndexRuleBinding {
	return schema.IndexRuleBinding(ts.schema)
}

// TopNAggregation returns the TopNAggregation which is not supported for traces
func (ts *traceSpec) TopNAggregation() []*databasev1.TopNAggregation {
	return nil
}

// EntityLocator returns the partition.EntityLocator which is not yet implemented for traces  
func (ts *traceSpec) EntityLocator() any {
	// TODO: Implement when trace partitioning is supported
	return nil
}