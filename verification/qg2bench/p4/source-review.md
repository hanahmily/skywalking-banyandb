# P4 aggregate composition source and semantic review

P4 follows the measurement contract in `/mnt/d/banyandb-ql-design/docs/bydbql-performance-verification-plan.md`
and its work-package companion:64/8192 rows, eight groups, independently generated
`value=prior+1`, direct SUM, COALESCE(SUM,0), computed SUM, changed equal-work
bindings/cache bypass, native eligible float control. A direct-versus-computed
cost is feature work, not proof of regression. Add broader group/repeated-input
probes only if measurements identify those owners.

## Setup, execution and current reuse

| Owner | Existing behavior | Attribution implication |
|---|---|---|
| `pkg/bydbql/plan_measure_reuse.go:54`, `plan_measure.go:257` | Template BindParams binds its immutable prepared statement; Bind clones the request and constructs execution-local state. | A compiled-plan cache hit still incurs binding/setup. It does not eliminate request validation or execution construction. |
| `pkg/bydbql/plan_measure_aggregate_input.go:37` | Compilation deduplicates an identical function/decimal-scale/expression recipe. Repeated SUM(v+1) shares one aggregate call; SUM(v+1) and COUNT(v+1) retain separate inputs. | Sharing across different functions could only be proposed after evidence; same-call dedup is already implemented. |
| `plan_measure_aggregate_input.go:105` | Each bind resolves the bound input expression and appends execution-local GroupExpressions outputs. | ResolveExpression work on cache hit is distinct from cache compilation. |
| `pkg/query/ql/group_expression.go:27` | Adds computed inputs before aggregation, forwards needed grouping/call source columns, and assigns group-expression metrics. | Use GroupExpressionExecutionStats for SUM(prior+1), not only final projection statistics. |
| `pkg/query/ql/expression_operator.go:304` | Constructs query-local executable nodes/closures; scalar constant kernels cache once within this operator. | Operator compile is setup work even when immutable compiled-plan cache hits. |
| `expression_operator.go:432` | Typed int/float columns lend their backing data; protobuf field/tag columns decode eligible rows. | Compatible typed input forwarding already exists. Do not propose avoiding an unconditional copy without showing an actual copied path. |
| `expression_operator.go:476` | Buffer capacity is reused when sufficient, clearing validity bits; growth reserves new storage before releasing old storage. | BufferBytes/FullColumnAllocations are cumulative allocation work, not live peaks. No per-batch allocation is needed when capacity fits. |
| `expression_operator.go:605` | Close releases reservations and closes source once; clearBatch clears retained reference slices and per-batch payloads; NextBatch closes on cancellation/errors. | Buffers belong to an execution, not the immutable template or a cross-query pool. |
| `pkg/bydbql/plan_measure_pushdown.go:58` | Computed aggregate inputs, patterns, residual filters and unsuitable entity/range shapes disable native reduction. Outer COALESCE alone does not. | Eligible direct-float SUM and float COALESCE need observed NativeAggregateExecutionStats, not just eligibility. Integer fallback and computed SUM must be reported as their observed routes. |

COALESCE(SUM) evaluates after final aggregation; it processes the scalar result
or eight output groups, rather than every input row. Computed SUM evaluates on
selected source rows before aggregation. These stages have different counters.
Expression metrics distinguish vector rows, selected conditional-arm rows,
constant evaluations, cumulative buffer capacity and live reservations; none
provides measured compiler-only allocated bytes or CPU time by itself. Profiles
must identify bind, operator construction, expression kernels and aggregation
separately; overlapping cumulative CPU stacks cannot be added.

## Existing focused checks

Thirty top-level focused tests passed in `pkg/bydbql` and `pkg/query/ql` with
querymetrics. Raw output: [source-tests.txt](source-tests.txt). Command:

```sh
go test -tags=querymetrics ./pkg/bydbql ./pkg/query/ql -run 'TestMeasure(AggregateExpressionRetainsNativePushdown|ComputedAggregateInput|SQLAggregateBindingDoesNotMutateCachedTemplate)|TestAggregate(ExpressionFinalizesBeforeCoalesce|CompositionReleasesOnCancellationAndBudgetFailure|IntegerSumOverflowAndFloatAverage|ScalarSQLNullsAndSharedState|ScalarAndGroupedEmpty|CancellationAfterRetainingGroupsReleasesState)|TestExpression|TestGroupExpression' -count=1 -v
```

Coverage includes immutable changed bindings, computed input sharing, native
float eligibility, all-NULL/empty aggregate behavior, finalization before
COALESCE, selected-row/three-valued masking, skipped division-by-zero and invalid
casts, checked integer arithmetic/sum overflow, source ownership on errors,
and budget/cancellation cleanup. `TestAggregateCancellationAfterRetainingGroupsReleasesState`
checks cancellation after state was retained; the composition-specific immediate
cancellation test alone does not prove cancellation of an in-flight RPC.

The previous combined QG2/QG3/pattern161-spec report is historical. Its production
source predates current P1 changes, so it is not relabeled current equivalence.
Fresh generated QG2 qualification is run through both current real QL RPC
integration suites with `-ginkgo.label-filter=QLB-T07 -ginkgo.focus=qg2_` and
`-ginkgo.fail-on-empty`, sequentially. Independent generated literal answers
cover full, NULL-only and empty windows; normal/bypassed caches and changed
bindings; scalar/grouped/repeated aggregates, computed input, float promotion,
COUNT DISTINCT input, HAVING and ordered aliases. Source: `test/cases/measure/qlgen/qg2_generate.go`.
The generated six-row fixture and existing replicated two-shard integration group
are semantic witnesses, not the64/8192-row performance fixture. The distributed
suite uses its existing two-data-node setup. The integration wrapper asserts one compile then hits, separate bypass requests,
consumed rows/groups and returned memory ownership; raw native results are not
its oracle. Commands, binary hashes, exact observed spec counts and reports are
retained by [verify_semantics.py](verify_semantics.py).

Current outcome: **12/12 specs passed in each topology**, standalone13.397s and
distributed28.625s. These are10 successful scenario families with3 bindings each
in2 cache modes (**60 successful verification RPCs/topology**), plus2 expected
overflow rejections/topology. Counts exclude Ginkgo lifecycle/report nodes.
[Correctness manifest](qg2-correctness.json) records both binaries and raw hashes;
`*-qg2.json.gz` and `*-qg2.txt.gz` preserve exact raw reports/logs compressed.
Their recorded raw hashes apply after decompression. No production changes.
