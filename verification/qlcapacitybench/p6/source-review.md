# P6 complete export contracts and ownership

P6 follows the authoritative performance verification plan and work packages
in `/mnt/d/banyandb-ql-design/docs/`. A4 requires export capacity and A5 a stable
secondary group-key ordering. It does not promise retained-result paging,
flat page latency or response metadata absent from the existing protocol.

The agreed complete export is:

```sql
SELECT service_id, COUNT(DISTINCT entity_id) AS distinct_count
FROM p6bench.m
WHERE time >= ? AND time < ? AND value::FIELD > 0
GROUP BY service_id
ORDER BY distinct_count DESC, service_id ASC
```

No LIMIT/OFFSET is used. N output service groups have1+(groupIndex%3) entities,
each with two positive timestamps. Source rows, distinct(group,entity) pairs
and output groups therefore differ. `entity_id` is storage-only, is the entity,
and has no index; `service_id` is searchable. The independent oracle enumerates
all N unique groups and their exact counts, then orders count descending and
service name ascending. Equal counts require the service key tie-break. Partial
page checks cannot substitute for comparing every returned row and byte count.

## Admission and output boundaries

- `pkg/bydbql/plan_measure.go:324` gives extended aggregate scans MaxUint32
  source limit, so the legacy100-row default is not a complete-export cap.
- `pkg/query/ql/sort.go:108` selects full sort when LIMIT is nil. With LIMIT,
  candidate K is uint64(OFFSET)+uint64(LIMIT), preventing uint32 wrap; early
  pages still require computing eligible input groups. LIMIT0 is special.
- `api/proto/banyandb/measure/v1/query.proto:52` carries data_points, trace and
  group_statuses. It has no total/truncation marker. Group gate statuses must
  not be described as a complete-row count. A separate count request is not
  atomic with an export under concurrent changes.
- Default query allowance is256MiB (`pkg/query/vectorized/measure/config.go:39`).
  The process protector uses default75% of observed cgroup memory unless
  overridden. These budgets have different scopes.
- The pinned gRPC client's default receive maximum is4MiB. Liaison
  `--max-recv-msg-size` default16MiB is for incoming requests, not exported
  responses. A client receive rejection is a response boundary, distinct from
  server query-memory rejection. P6 keeps that default and records the result;
  raising it cannot turn this campaign's failure into acceptance.

The sequence is complete1k output qualification, then10k and100k output probes
at fixed budgets. Stop growth at the first reproducible admission/response
boundary. Keep rejection evidence separate from successful latency, assert
cleanup and run a known small recovery query. Do not time repeated failures or
replace the complete export with several pages.

## Ownership/counter interpretation

| Owner | Existing behavior and available evidence |
|---|---|
| Native source | Native input/frame rows in BatchExecutionStats; ScanResourceSnapshot cursor refs, results, scan reservations and decode tasks. Idle decode workers are infrastructure. The snapshot is a live process-wide gauge, not per-query historical peak. |
| Exact grouped distinct | AggregateExecutionStats exposes input rows, groups, group keys, fixed state capacity and aggregate live reservations. COUNT DISTINCT per-key counts/capacity exist in operator AggregateStats but are **not** exported by process AggregateExecutionStats. DistinctExecutionStats describes SELECT DISTINCT and must not be relabeled COUNT DISTINCT state. Mark inaccessible exact-key ownership null. |
| Ordering | SortExecutionStats includes accepted/evicted candidates, peak candidates, payload/capacity, output and reservation work. Completion peaks/capacities accumulate over successful executions; divide deltas by executions, while LiveBytes is a gauge. Full export owns all candidates; candidate values copy required payload, avoiding pinning entire source batches. |
| Sort output | `pkg/query/ql/sort.go:508` reuses an output batch up to1024 rows, resetting before each emission. This does not imply the final RPC response streams to the client. |
| Protobuf response | `pkg/query/vectorized/measure/adapter.go` retains serialized output reservation until transport completion; closing the source is not sufficient to free already materialized response ownership. |
| Encoding/transport | `pkg/query/vectorized/query_memory_scope.go:192` reserves encoding and transport copies beside retained protobuf output (twice bounded wire size, with exact fallback near a boundary), held until RPC completion. Cumulative reservation charge is not live memory, and proto.Size is wire size rather than Go heap footprint. |

No production instrumentation endpoint is added. Overall reservation peak,
per-owner counters where exposed, profiles, process heap and cgroup footprint
must retain their different scopes. Missing per-owner high-water marks are null;
source, aggregate, ordering and output peaks cannot be summed as simultaneous
live residency without a common observation.

## Existing focused verification

Forty top-level tests passed across four packages with querymetrics. Raw output:
[source-tests.txt](source-tests.txt). Command:

```sh
go test -tags=querymetrics ./pkg/query/ql ./pkg/query/vectorized ./pkg/query/vectorized/measure ./pkg/bydbql -run 'TestSort|TestRawSort|TestResponseEncoding|TestQueryMemoryScopeUsesProcessorLimitAtCaller|TestFrameEgress|TestMeasureSort' -count=1 -v
```

These cover typed lexicographic order/ties, completed-group ordering, hidden
keys, complete-distinct-before-page semantics, full sort versus top-K, LIMIT0,
wide OFFSET arithmetic, candidate rejection/recovery, source errors/cancellation,
response encoding against retained output, transport-held reservations, and
frame encoding rejection/cancellation. They are semantic/ownership evidence,
not measurements of a100k-output deployment.

`topology_test.go` supplies the same export harness with either standalone or
two actual data nodes and a liaison. Distributed qualification requires two
shards and zero extra replicas; `verifyWorkerRows` asserts distinct worker trace
identities, positive contributions and the independently known total raw rows.
These are real RPC servers sharing one Go process, so counters do not isolate
per-worker/coordinator CPU or reservations. Small both-topology complete-result
qualification and the growth outcome belong to the harness report.
