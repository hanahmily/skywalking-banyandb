# P1 local count scans and reusable decode storage — 2026-10-29

This follow-up addresses the standalone NOT IN/CASE capacity failure left by
[the membership fixes](improvement-results.md). Those earlier results and the
[original diagnosis](memory-diagnosis.md) remain historical evidence. The new
comparison baseline already contains compact membership and empty-input pruning;
it is not the original expression-expansion implementation.

## Reproduced cause

The unchanged baseline fails the real QL correctness RPC at
`p1_not_in_binding_0` with `ResourceExhausted: query memory pool exhausted` in a
2-CPU, 4-GiB container. A timestamp merge must open a frontier spanning 5,120
series before returning rows. The shared block bound is 34,656 bytes for each
small projected block, in addition to workspace.

Changing traversal alone is insufficient. Admission charges are monotonic:
releasing a cursor refunds its live tracker reservation but not the query's
cumulative allowance. Charging fresh decoded storage for every series would
consume 177,438,720 bytes, beyond the native 96-MiB allowance in this container.
A separate native reproducer with 128 series fails under a 16-MiB monotonic lease.
Both constraints therefore require attention: simultaneous frontier width and
repeated allocation/charging across windows.

## Implementation

`RawPlan.CanScanSeries` proves a deliberately narrow ordering contract. It admits
COUNT and COUNT DISTINCT with nonfallible expressions. Grouped results also need
an existing final ORDER BY covering every grouping key. It checks hidden
aggregate calls and excludes buckets, outer DISTINCT, SUM/AVG, MIN/MAX, fallible
arithmetic/casts and incomplete group ordering. Float group keys are already
canonicalized for signed zero and NaN, so changing first encounter does not
change their displayed representation.

An in-process hint selects the existing series-window scan for complete local,
single-group raw input. Within a series, timestamp merging and newest-version
deduplication remain in force. Raw/protobuf queries, distributed queries,
multi-group queries, index-mode requests and unproven QL plans retain their
existing route. No protobuf or persisted format changes are involved, and no
source-row in-memory sort is introduced.

A query-owned decoded-storage slot retains its conservative shared reservation
until closure. Reusing it transfers actual timestamp/version, column and decoder
buffers, and refills a reusable byte arena. An idle slot must cover the next
block's row bound and uncompressed-byte bound independently. Otherwise another
reservation is required. Duplicate source projections retain ordinary per-cursor
accounting because a block's payload bound counts each stored column once.

The existing tracked native cursor pool accounts for held cursors. A slot cannot
be reused until the cursor's last borrowing batch releases it. Closing a scan
releases idle slots, while outstanding batch owners retain their slot and charge
until release. Active native/node reservations and the monotonic admission
contract remain unchanged. This is actual buffer reuse, not recycling accounting
credit or increasing limits. Variable block sizes and overlapping versions can
still require additional slots and legitimately exhaust the budget.

## Verification and measurements

The frozen production build passes all four generated P1 scenarios in both
standalone and distributed modes: 48 verified requests per topology, including
cache hits/bypass, changed bindings, empty/NULL cases and overlapping CASE lists.
The standalone RPC harness independently passes those same 48 calls under the
96-MiB native allowance. All ten feasibility arms return both expected cohorts
without rejection (20/20 calls).

The selected compatibility cohort passes **568/568 specs on each topology**.
It covers raw batch behavior (T03), complete aggregation (T07), native aggregate
pushdown and buckets (T08), HAVING (T14), DISTINCT (T15), multi-key grouping (T16)
and ordering/pagination (T17). No distributed execution-order change is made. Integration keeps its existing
128-MiB native allowance, with two CPUs/four GiB standalone and four CPUs/eight
GiB distributed across two workers; the focused standalone RPC harness and timing
keep the stricter 96-MiB native allowance.

The native measure, QL/binder, dispatcher, processor and liaison unit packages
pass. The nine new native cases cover actual backing-pointer transfer, pins
surviving subsequent pulls/cancellation/source close, version deduplication,
reservation rejection before decoding, failed prefetch cleanup, larger windows,
duplicate projections, and the original monotonic-lease reproducer. Focused race
checks also pass with released-buffer poisoning enabled. Scoped lint reports zero
issues; formatting, generated-suite/harness tests and license checks pass.

### Fixed benchmark protocol

Timing is limited to P1. Four previously admitted controls (IN 10, IN 1,000,
IN 10,000, and IN 10 with cache bypass) use five paired fresh processes, alternating
baseline/candidate order. Each arm has 20 warmups and 200 successful timed RPCs
per process: 8,000 timed control RPCs. The baseline is the frozen membership-fix
binary from `/tmp/ql-p1-fixed`.

Two newly admitted representative workloads, NOT IN 10 and sixteen-branch CASE,
use five fresh candidate processes with the same warmup/sample counts: 2,000
additional timed RPCs. Baseline rejection times are not successful-query latency
samples, so no before/after speedup is claimed for these workloads. Other list
sizes and CASE shapes retain correctness and memory coverage without expanding
the timing matrix.

All timing uses two CPUs, four GiB, no extra swap and GOMAXPROCS=2. The native
allowance remains 96 MiB and the vectorized tracker remains 256 MiB. Builds,
unit/integration tests, profiles and forced GCs run outside timing. Process-level
summaries, not individual RPCs, are the independent observations. No post-hoc
percentage gate or extra rounds are used to obtain a preferred verdict.

### Previously admitted controls

Means of five process summaries; MB is decimal millions of bytes. CPU and
allocation cover the whole client/server process. Every timed request succeeds,
with the same answers and 80 native input rows per RPC on both versions.

| Query | p50 before → after (ms) | p95 before → after (ms) | CPU/RPC before → after (ms) | Allocated MB/RPC before → after |
|---|---:|---:|---:|---:|
| IN 10 | 35.109 → 34.839 | 37.734 → 36.841 | 37.311 → 36.949 | 4.112 → 4.090 |
| IN 1,000 | 35.745 → 35.383 | 38.633 → 37.935 | 38.063 → 37.732 | 4.531 → 4.508 |
| IN 10,000 | 40.767 → 40.504 | 44.253 → 43.863 | 44.590 → 44.371 | 8.736 → 8.656 |
| IN 10, cache bypass | 34.997 → 35.096 | 37.507 → 37.431 | 37.118 → 37.239 | 4.141 → 4.121 |

Paired p50 changes are −0.270 ms (95% process-pair bootstrap interval
−0.543 to −0.037), −0.361 ms (−0.576 to −0.147), and −0.263 ms
(−0.512 to −0.104) for cached IN 10/1,000/10,000. Bypass changes by
+0.100 ms, interval −0.155 to +0.347; its direction remains uncertain.
These are modest changes from five local process pairs, not a production SLO or
proof that all queries improve. They do not replace the distinct baseline and
small-query costs recorded in the earlier membership comparison.

### Newly admitted broad queries

All 2,000 timed RPCs succeed. Each scans 10,240 native rows, with two alternating
binding cohorts checked against the fixture oracle. These are successful-query
costs on the candidate; the baseline has no successful latency for comparison.

| Query | Mean process p50 (ms) | Observed process p50 range (ms) | Mean process p95 (ms) | CPU/RPC (ms) | Allocated MB/RPC |
|---|---:|---:|---:|---:|---:|
| NOT IN 10 | 95.012 | 93.952–96.535 | 105.656 | 122.747 | 21.020 |
| CASE, 16 branches | 96.773 | 95.844–97.378 | 107.141 | 124.614 | 22.816 |

The result is removal of the demonstrated capacity failure. These broad scans
still cost more than the index-pruned IN controls, which read only 80 rows.
Nothing here establishes production-scale capacity or justifies scanning without
selective indexes when the workload can use them.

### Memory and lifecycle

All ten isolated memory probes return correct results with zero live reservations,
zero native cursor/result/owner references and zero pending decode tasks at the
checked end state. Named pool reference counts return to their starting values.
The final underway CASE cancellation returns Canceled and reaches the same idle
state, followed by a successful query on the same cluster. Eight simultaneous
sixteen-branch CASE requests all succeed, followed by successful recovery.

| Query | Peak tracked reservation | Cumulative lease charge for isolated RPC |
|---|---:|---:|
| IN 10 | 8,364,701 B | 8,363,642 B |
| IN 1,000 | 8,444,509 B | 8,443,450 B |
| IN 10,000 | 9,164,509 B | 9,163,450 B |
| NOT IN 10 | 38,035,661 B | 38,066,830 B |
| NOT IN 10,000 | 38,835,469 B | 38,866,638 B |
| CASE, 16 branches | 38,517,369 B | 38,562,123 B |

These are conservative reservations, not physical heap or allocations per RPC.
The admitted IN controls do not all reduce tracked peak memory: the previous
membership-only build recorded 8,357,229 / 8,373,069 / 8,517,069 B for IN
10/1,000/10,000. Retaining reusable workspaces can keep reservations live longer;
this campaign has not isolated the entire difference, so it is not presented as
a universal memory reduction. The broad query's 38.5-MB peak fits without raising
the native 96-MiB allowance. Eight concurrent CASE requests peak at 178,809,780 B
in the final lifecycle run (226,198,230 B in the initial run); that aggregate
peak depends on overlap and scheduling, not a new per-query limit.

The first cancellation probe exposed a **harness observation race**: an instant
zero check was followed by a separate snapshot showing 2,144 live bytes while
the canceled server request was still finishing; native scan resources were
already idle. That original evidence is preserved. The harness now requires a
20-ms quiet interval and records the snapshot that satisfied the check. The
rerun records zero live memory and idle native resources after cancellation.
The successful-query and timing logic is unchanged; only untimed ownership
checks were tightened. This is a bounded lifecycle check, not a long-running
heap-leak soak. One-GC heap differences are retained in the raw observations
without equating pooled heap retention with a leaked owner. The native protector's
private reservation counter is not directly observed; tracked ownership and
same-cluster recovery provide the lifecycle evidence.

## Scope and remaining work

The demonstrated P1 standalone NOT IN/CASE capacity gap is resolved for this
fixture. The new QL reordering permission is local and count-only. Native buffer reuse
also applies to existing non-time-ordered window scans with a shared tracker,
including existing series/index-order callers; it preserves their visit order.
Other QL plans and the distributed merge retain their ordering contracts. No new consistency repair,
protobuf changes, spilling, source-row sorting or relaxed memory protection is
introduced. Larger DISTINCT cardinalities, wider/changing blocks and large
within-series version frontiers can still hit legitimate memory limits.

P2's cross-shard/stage capacity investigation remains a separate task. P3–P6
are also unchanged investigation plans. CASE cache benefit is not quantified by
this campaign: sixteen-branch CASE timing is cached only; bypass still has
correctness/memory coverage. No decision or limit increase is required to retain
this scoped fix. Changes remain local and uncommitted.

## Evidence and reproduction

[Evidence directory](series-results/) contains the unchanged baseline/candidate
manifests, Docker commands and observed resource settings, individual latency
samples, comparison summaries, compatibility spec outcomes, memory observations,
checks and the preserved red result. Large binaries and verbose logs remain in
the recorded `/tmp/ql-p1-series*` locations.

- Baseline source: `5c80d0f110a3ffd918fbaf4d0e985f97b3e9eebdc597c154ccfb15d8da1d3975`.
- Timed candidate source: `9588c885ebee5af3320fce662a9d274c32ba0c6dd581443f66e4b43f06f4799a`.
- Lifecycle-harness revision: `3616ab4ec7e0749937e284c4818e12f9efa6b2ae041b8fe7fc03fbd125831ff0`.

The last revision changes only the ownership checks in the verification harness.
All Go/module sources outside `verification/` have identical recorded hashes;
the standalone/distributed integration binaries are also byte-identical between
those two builds. Performance measurements therefore retain their frozen build
identity rather than being relabeled as measurements of a different harness.

Representative commands (source `/tmp/pr4-env.sh` for this machine's pinned Go):

```sh
python3 verification/qlcapacitybench/run.py build --output /tmp/new-p1-build
python3 verification/qlcapacitybench/run.py correctness --output /tmp/new-p1-build
python3 verification/qlcapacitybench/run.py feasibility --output /tmp/new-p1-build
python3 verification/qlcapacitybench/run.py memory --output /tmp/new-p1-build --probe-arm case_16
BYDB_POISON_RELEASED_CURSOR=1 go test -race ./banyand/measure -run 'TestSeriesScan|TestTimeOrderedScanRetainsPerCursorAdmission' -count=1
```

The exact five-pair and five-process commands, schedules and arm selections are
preserved with their run records. Replaying frozen timing binaries does not
require rebuilding the source. No additional rounds were run to change a verdict.
