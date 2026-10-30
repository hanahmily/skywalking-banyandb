# P2 exact distinct across shards and storage stages — 2026-10-29

P2 (Q2/A3/A6/A7/A9) now has actual QL RPC evidence, a bounded capacity report and
no production change. Distributed passes all **48 verification RPCs / 3 specs**;
real hot/warm/cold migration produces the exact overlapping unions. Standalone
returns **40 correct results and 8 native ResourceExhausted errors**, so its full
SUM/combined cohort remains **failed capacity qualification**. All eight separate
count-only capacity arms admit, including D=50,000; all **1,200 timed RPCs** succeed.
This is not universal capacity qualification or a before/after speedup claim.

## Correctness and actual stages

The generated `QL-P2` cohort reuses P1's unchanged `qlperf.activity` schema:
unindexed routing `storage-only.entity_id`, keyword-indexed nonrouting
`searchable.service_id`, 10,240 rows, 4,096 positive entities and 1,024 services.
The independent oracle enumerates fixture rows and sets without calling the
parser or execution operators. Scalar, grouped and combined shapes cover full,
selected and empty inputs, with cache and bypass: 24 cases, 48 requests per
topology. Full answers are entity distinct 4,096, service distinct 1,024 and
SUM(value)=8,192; each service group has 4 entities, 1 service and SUM 8.

Normal routing is preserved. The singleton repeated-service witness has ten raw
rows: eight positive and two idle. The distributed trace records **4 and 6 raw
rows** on workers `data-127.0.0.1:35863` and `data-127.0.0.1:43031`. Both exceed
the two idle rows in the entire witness, proving both contribute positive rows.
Its complete QL result is SUM 8, entity distinct 4, service distinct 1. All 65
readiness pages/requests pass on the authoritative distributed run.

The standalone failures are exactly the broad SUM scalar/grouped and combined
scalar/grouped shapes, repeated under cache/bypass (2/2/4 rejections). Count-only,
selected and empty cases return correct results. The existing count-only scan
proof deliberately excludes SUM; these broad queries must retain the timestamp
frontier. An isolated full-combined rejection yields **zero native input rows**,
locating the boundary before distinct state receives input. Its responsible
owner is native timestamp merge/decode admission, consistent with the retained
[P1 diagnosis](series-results.md). Shrinking the distinct set would not repair
that source boundary. No ordering permission or admission limit was changed.

The stage checkpoint starts three actual data nodes with hot/warm/cold selectors,
configures 3/7/30-day stages, writes six rows at distinct timestamps, then executes
hot→warm and warm→cold lifecycle commands with explicit node labels. Its stable
rows are hot={a,b}, warm={b,c}, cold={c,d}. Real stage-selected QL RPCs give:

| Selection | COUNT DISTINCT entity | SUM(value) |
|---|---:|---:|
| hot |2| — |
| warm |2| — |
| cold |2| — |
| hot,warm |3| — |
| hot,warm,cold |4|6|

Before migrations hot COUNT is 6; after the first migration warm COUNT is 4.
Stage inclusion therefore changes actual observed rows. The oldest rows are 12
days old to exceed the accumulated 10-day TTL at whole-segment boundaries.
The frozen Docker checkpoint passes in 8.4 seconds; no migrations occur during
timing and no lifecycle/storage bug was found. [Stage evidence](p2-results/stages.json).

## Fixed cost/capacity protocol

The [protocol](p2-protocol.md) was declared before timing. The primary shape is:

```sql
SELECT entity_id, COUNT(DISTINCT service_id) AS n
FROM qlperf.p2_<arm>
WHERE time >= ? AND time < ?
GROUP BY entity_id ORDER BY entity_id ASC
```

Each routing entity is one group. Service keys vary at unique millisecond
timestamps; one key belongs to one group, so D counts distinct(group,key) entries
across the result. The independent oracle enumerates that key domain. R total
rows is verified separately through QL COUNT before each arm. Complete result
maps are checked structurally on every RPC, including every timed request.
Each process also verifies generated combined control `p2_combined_0_selected`
(SUM 16, entity distinct 8, service distinct 2).

There are five fresh processes, three warmups and 30 successful timed RPCs per arm
per process, with rotated arm order. Each arm also has a separate untimed
qualification RPC before those warmups. Primary p50 is the median of 30 samples;
p95 is nearest rank ceil(0.95×30). The table reports means of the five process
summaries. CPU and allocation cover the client/server process. MB is decimal;
tracked reservations are conservative accounting, not heap bytes. No forced GC,
build, test or profile run overlaps timing. No extra rounds or gates were added.

| Arm | R/D/G/W (W in B) | p50 ms | p95 ms | CPU ms/RPC | Allocated MB/RPC | Isolated tracked peak B |
|---|---|---:|---:|---:|---:|---:|
| d1000 | 50,000/1,000/8/16 | 12.300 | 17.376 | 19.083 | 4.496 | 97,965,123 |
| d10000 | 50,000/10,000/8/16 | 12.721 | 18.265 | 17.865 | 5.011 | 98,157,315 |
| d50000 | 50,000/50,000/8/16 | 15.432 | 21.435 | 21.242 | 6.980 | 99,749,509 |
| r10000 | 10,000/1,000/8/16 | 3.012 | 5.116 | 4.750 | 1.146 | 24,666,026 |
| r100000 | 100,000/1,000/8/16 | 22.805 | 37.922 | 37.534 | 9.047 | 91,865,312 |
| g1 | 50,000/1,000/1/16 | 11.492 | 17.216 | 16.357 | 3.912 | 77,695,352 |
| g64 | 50,000/1,000/64/16 | 12.547 | 23.789 | 19.633 | 1.813 | 23,944,001 |
| w256 | 50,000/1,000/8/256 | 14.514 | 26.121 | 24.271 | 6.789 | 93,154,322 |

All 150 timed requests per arm succeed; no rejected latency enters the table.
D=1k/10k/50k holds R/G/W fixed. Separate arms vary R, G and W at D=1k. No
reproducible distinct-state boundary was found within the declared envelope;
growth stopped at 50k as planned. The nonmonotonic tracked peaks across R/G are
sensitive to source block layout and reuse; they cannot establish per-entry
reservation slopes or imply larger R always needs less memory. No unbounded
capacity or precise per-group live residency claim follows from these values.

The two-CPU/four-GiB benchmark keeps GOMAXPROCS=2, no extra swap, the existing
256-MiB vectorized tracker and cgroup-derived 96-MiB native allowance. Integration
retains P1's explicit 4-GiB protector setting (128-MiB native allowance), with
2 CPU/4 GiB standalone and 4 CPU/8 GiB distributed. The three-node stage correctness
checkpoint uses 4 CPU/8 GiB and default policy. Exact Docker commands, inspected
resource settings and observed cgroups are retained for every run.

The full standalone qualification is still failed. Before timing, the runner's
explicit `--allow-standalone-admission-gap` exception verified exactly three
capacity-assertion failures, 48 attempts and 8 ResourceExhausted entries; it also
required the distributed pass and admitted independent capacity arms. This is
recorded in [exception evidence](p2-results/standalone-admission-exception.json).
It does not classify rejected SUM requests as correct or time them.

## Memory ownership, failure and recovery

All eight isolated probes end with zero live tracked reservations, no native
cursor/result/owner references or pending decode tasks, and a 20-ms quiet interval.
The broad generated combined query reproduces native rejection, reaches the same
idle state, then the generated small combined control succeeds on the same
cluster. Underway cancellation returns Canceled and releases all observed owners.
Eight callers at the already admitted d1000 size all succeed, with aggregate
tracked peak 679,720,547 B across independent query allowances, followed by a
successful same-cluster recovery. This is bounded cleanup evidence, not a leak
soak or direct observation of the protector's private reservation counter.

Memory-only profiles use 4-KiB sampling and explicit GC outside timing. The D50k
allocation difference identifies native `blockCursor.loadData` (~6.39 MiB
cumulative), including decoded families/byte blocks, and QL `aggregateGroups`
hash growth (~2.49 MiB flat) and key chunks (~1.38 MiB flat) as relevant owners.
Those are expected source and exact-key allocations, not demonstrated redundant
copies. Raw profiles contain profiler self-allocation; both raw text and the
explicit `-ignore=runtime/pprof` view are retained. Filtered percentages still use
the original total denominator. Sampled allocation is neither retained heap nor
peak live reservation. Profiled isolated allocations exceed warm timed values;
they are different measurement protocols and are not combined into a speed claim.

Standalone source batches use the local native boundary; the distributed
correctness traces demonstrate real worker frame transport and coordinator union.
Per-owner live source/transport/coordinator residency and distinct reserved bytes
per entry were **not directly captured** and remain null, rather than inferred
from process-wide peaks or sampled allocation. No distributed timing curve or
large-stage timing campaign is claimed. These limits prevent promoting an owner
optimization from this report; the native-frontier boundary and admitted exact
capacity envelope are the supported findings.

## Reproduction and provenance

Authoritative execution: `/tmp/ql-p2-final-campaign`; bounded portable evidence is
in [p2-results/](p2-results/). Large frozen binaries, verbose logs and sampled
allocation profiles remain in that directory, with profile hashes in the report
artifacts. The source revision is `5e78df39d0ca171aa61b97dbb4383dab32c434e6`
plus only the local verification/generated-case changes. Source digest:
`691777509954546fc89806af2e9657bf21c2b7e4560714f314c5d4559d6d23b1`.
Go is 1.26.10 with querymetrics; frozen image is
`sha256:09c665d7ce2302ef883da73a11f72161ee18a66336dfb7d03f52fd817875d877`.
The [manifest](p2-results/manifest.json) records all three binary hashes, runner
hash, source hash, fixed dimensions and protocol. Per-process fixture row,
request/binding and oracle hashes are in the raw JSONL records. The P1 fixture
schema/rows digest is `cde02cbfca428d7d4069cbbba68f48af6fbce6c93da7c9c8fab50e3b7df650c8`.

```sh
source /tmp/pr4-env.sh
python3 verification/qlcapacitybench/p2_run.py build --output /tmp/new-p2
python3 verification/qlcapacitybench/p2_run.py correctness --output /tmp/new-p2
python3 verification/qlcapacitybench/p2_run.py stages --output /tmp/new-p2
python3 verification/qlcapacitybench/p2_run.py feasibility --output /tmp/new-p2
python3 verification/qlcapacitybench/p2_run.py memory --output /tmp/new-p2
python3 verification/qlcapacitybench/p2_run.py measure --output /tmp/new-p2 --allow-standalone-admission-gap
```

`correctness` intentionally returns failure for the recorded standalone admission
qualification while still executing distributed. Zero selected specs is rejected.
Scoped lint reports 0 issues; changed Go files pass repository gci/gofumpt, focused
P1/P2 generator tests, harness compile checks and diff checks. An optional full
qlgen run was deliberately interrupted after roughly 4.5 minutes and has no pass
result. No unrelated full integration/build campaign was run.

The [initial evidence](p2-results/initial/) preserves an incorrect harness raw-row
expectation (8 vs actual 10 including idle rows), not a QL result defect. Corrected
worker assertions and lint/cleanup harness revisions were frozen before timing;
preliminary frozen directories were not relabeled as the authoritative run.
No production fix, commit, push or external publication was performed. P3's
current LIKE/ILIKE investigation remains the next independent package.
