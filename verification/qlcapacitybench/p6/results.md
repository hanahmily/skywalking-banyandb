# P6 complete-export capacity boundary

The source-shaped complete export verifies **1,000 output groups**, then rejects
**10,000 output groups twice** under unchanged default budgets. Both rejections
release all observed owners and recover on the same cluster. The declared stop
rule leaves **100,000 output groups unattempted and unqualified**. This is a
completed bounded capacity assessment, not acceptance of the 100k requirement.
Evidence identifies admission-lease exhaustion while materializing sort candidates
after aggregation. It does not establish redundant copies or another avoidable
cost, so **no production change is justified**.

## Scope and reproducibility

Measured revision `1af21ec71f2b98bae44371e84e279b58929cc200`, Go1.26.10,
querymetrics. [Frozen manifest](evidence/manifest.json) records full production,
generated API and harness source hashes, runner/protocol hashes, binary hash and
Docker image digest. [Protocol](protocol.md) fixes one complete export shape and
one staged sweep; no ordinary-query baseline or latency comparison was intended.
Historical capacity evidence remains separate.

Schema `p6bench.m` has unindexed entity `storage-only.entity_id`, keyword-indexed
`searchable.service_id`, and integer value/summation/count fields. Two naturally
routed shards, no replicas and no sharding key preserve the source-shaped contract.
Group g is `s%06d`; its 1+(g%3) distinct entities are `e%06d_%d`. Every entity has
two samples at base and base+1s, with values1/2. Repeated identities across time
must count once. The fixed previous-hour base timestamp is in the manifest.

```sql
SELECT service_id, COUNT(DISTINCT entity_id) AS distinct_count
FROM p6bench.m
WHERE time >= ? AND time < ? AND value::FIELD > 0
GROUP BY service_id
ORDER BY distinct_count DESC, service_id ASC
```

There is no LIMIT, OFFSET or TOP horizon. Independent expected output enumerates
count tiers3,2,1 and ascending unique service keys. Every decoded row, count and
position is checked; missing or wrong rows fail correctness immediately. All writes
are acknowledged; three bounded indexed checks verify first/middle/last groups.
Successful source counters additionally verify every input row consumed. Fixture,
request, complete expected/output bytes and digests are retained. The full1k
output is identical in both topologies; its canonical JSON is32,001B and complete
protobuf QueryResponse is67,004B. These are actual full response sizes, not pages.

```sh
python3 verification/qlcapacitybench/p6/run.py build --out /absolute/fresh-output
# Exclusive lane: no concurrent team builds/tests/campaigns.
python3 verification/qlcapacitybench/p6/run.py qualify --out /absolute/fresh-output
python3 verification/qlcapacitybench/p6/run.py sweep --out /absolute/fresh-output
python3 verification/qlcapacitybench/p6/summarize.py /absolute/fresh-output
```

Scoped lint and independent oracle unit test pass. [Source review](source-review.md)
records40 focused sort/response tests. The same binary first qualifies1k complete
output through two real data nodes plus liaison: the trace verifies1,968+2,030
raw input rows. This qualification uses one4CPU/8GiB container, no extra swap,
and default256MiB allowance per node. Those nodes share the test process, so
process-wide counters aggregate all nodes; they are not a liaison-only measurement
or a distributed capacity sweep.

Standalone stages use fresh2CPU/4GiB containers, CPU affinity0/1, GOMAXPROCS2,
no extra swap,256MiB live query tracker and default75% process protector.
The gRPC client retains its default4MiB receive limit; the server's16MiB incoming
request limit is separate. No limit was raised. Actual cgroup settings, load,
CPU usage and memory peaks are retained. No CPU throttling occurred.

## Complete results and first boundary

| Standalone target output groups | Input rows | Outcome | Full returned groups | Protobuf response bytes | Reset tracked peak B |
|---:|---:|---|---:|---:|---:|
|1,000|3,998|Complete, exact ordered answer|1,000|67,004|27,279,385|
|10,000 attempt1|39,998|ResourceExhausted|None|Unavailable|93,900,998|
|10,000 attempt2|39,998|Same rejection, reproducible|None|Unavailable|93,900,998|
|100,000|Not written|Not attempted after boundary|Unavailable|Unavailable|Unavailable|

Both rejected RPCs return exactly:
`rpc error: code = ResourceExhausted desc = query memory pool exhausted`.
The failure is not reported as a partial successful response. No rejected call
contributes to successful-query latency/throughput. The1k diagnostic call takes
44.246ms,56.731ms process CPU and14,501,344 allocated bytes. Rejected-call elapsed
397.229/435.460ms and allocation107,768,008/106,187,432B are retained only as failure
diagnostics, not benchmark observations. There are no p50/p95 claims from one call.
The1ms owner sampler contributes overhead; client/server/background costs are
co-located. Automatic GC remains enabled; forced GC is outside the RPC.

Both10k attempts have identical structural deltas:

- Source consumes39,998 rows; local aggregation completes10,000 groups.
- Sort receives7,168 groups across seven batches and accepts7,095 candidates,
  with no evictions, comparisons, completed sort or output rows.
- Native aggregate execution is zero. This is local distinct aggregation and
  full candidate materialization, not a native partial aggregate or response limit.
- Aggregate cumulative reservation/release is3,073,448B; sort cumulative
  reservation/release is4,883,716B. Both return to zero live ownership.

The last accepted stage of execution is therefore sort candidate construction
following complete aggregation. Failed sort completion snapshots are absent:
zero completed PeakCandidates/PeakBytes fields do not mean no sort memory.
Sampled sort live maxima are3,933,208/4,219,780B; sampled aggregate live maximum
is1,958,912B. These1ms samples are lower bounds, not exact or additive live peaks.

## Admission owner and memory interpretation

The raw error originates in `banyand/protector/query_budget.go`, not the tracker
error `memory budget exceeded`. Default protector limit is75% of4GiB=3GiB;
QueryBudget pool is limit/4=768MiB and MaxQuery is pool/8=96MiB
(100,663,296B). `AdmitMeasureContext` assigns that maximum and charges1MiB fixed
overhead. `MemoryTracker` lease charges are conservative and monotonic even when
live tracker reservations are released. These source-derived limits are distinct
from the256MiB refundable live tracker and from whole-process/cgroup memory.

Each rejected attempt records exactly99,614,418B of successful instrumented
lease charges. With1,048,576B fixed admission overhead this is100,662,994B,
**302B below the96MiB assignment**. Alongside identical candidate progress and
error, this strongly identifies the per-query monotonic admission lease as the
blocking owner during sorting. Direct reservation Used and the exact rejected
charge are not exposed by this harness and remain null in
[boundary-attribution.json](evidence/boundary-attribution.json); this is a
source-derived inference, not a fabricated live-lease measurement. Neither
93,900,998B global tracked peak nor99,614,418B cumulative lease charges is an
actual heap peak. The256MiB tracker, client receive limit and100k output size
were not reached as observed rejection boundaries.

For the admitted1k standalone query, aggregation accounts8,000B fixed state,
8,192B state capacity and53,296B group-key capacity. Sort completes1,000
candidates with capacity1,024 and observed completed owner peak770,528B,
including737,408B descriptors and28,000B payload capacity. Source-pin count is
zero, consistent with copied candidate payloads. COUNT DISTINCT key-capacity
is not separately exposed by AggregateExecutionStats; the fixed-state figures
do not represent all distinct-key memory. DistinctExecutionStats belongs to
SELECT DISTINCT and is not substituted. Response protobuf size is observed;
response live peak is unavailable. Existing response serialization reserves
encoding beside retained output, but no response boundary was reached here.

Every export attempt is isolated by20ms quiet ownership,GC,tracked-peak reset,
RPC,full verification where admitted,quiet cleanup andGC. All scan cursors,
decode tasks,reservations,pins,results,compile/uncached bytes and aggregate/
native/sort live gauges return to zero. Every attempt, including both failures,
then verifies the same cluster's indexed `s000000` group and repeats cleanup.
Cache retained plans remain intentionally owned. The rejection pair's retained
heap deltas are+10,799,344/+10,593,096B; these single post-GC snapshots include
pool/runtime reuse and harness verification effects and do not prove a leak.
No long-lived retention claim follows.

Standalone cgroup peaks are34,209,792B(1k) and111,845,376B(10k); they include
fixture,index,runtime and page cache. Distributed small qualification peaks
48,455,680B. Host1-minute load spans0.64–0.94 across recorded starts/ends;
affinity is not exclusive host ownership. All raw logs and attempted calls are
preserved. [Attempts](evidence/attempts.json) retain observed before/after owner
counters; [runs](evidence/runs.json) retain process/resource summaries.

The process-wide SortExecutionStats.FullSort flag is not populated by
recordSortCompletion. The summarizer therefore checks omitted LIMIT/OFFSET,
K=0,1,000 completed candidates and the entire ordered oracle instead of treating
that false flag as top-k evidence. This postprocessing correction is preserved
in [analysis-correction.json](evidence/analysis-correction.json); no server
measurement was rerun. [Artifact hashes](evidence/artifact-hashes.json) cover
portable raw and derived evidence.

This establishes an admitted1k example and a reproducible rejected10k example,
not the exact maximum between them. A4 total/truncation metadata remains absent;
a separate count would not be atomic with a page under writes. A5's unique group
key resolves ties within this query, without retained-result sessions or equal
first/middle/last-page latency guarantees. No paging substitute,100k acceptance,
budget increase or optimization claim is made.
