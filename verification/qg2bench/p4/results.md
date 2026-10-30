# P4 aggregate composition: cost, reuse and ownership

All **12,000 timed RPCs** pass on the fixed 64/8,192-row fixtures. All 16 isolated
single-query memory probes return to idle ownership, and eligible float SUM and
float COALESCE retain observed native reduction. The ordinary SUM base/head
comparison is **inconclusive**, not evidence of a regression or non-inferiority.
Computed SUM adds measured input-expression work and buffer capacity; current
within-execution reuse already avoids allocating on every batch. Profiles identify
setup and evaluation owners but do not prove avoidable work. **No production fix
is justified by this campaign.**

## Identity, correctness and reproduction

Head `c59b950f5a2bbbfa015475f2dfadae471e53275e` versus ordinary-query base
`39d33bc5d8b49ea7ee33c253de2b476648637669`, Go `go version go1.26.10 linux/amd64`, querymetrics tags,
image `sha256:09c665d7ce2302ef883da73a11f72161ee18a66336dfb7d03f52fd817875d877`. Both binaries use identical fixture/campaign
harness files. The isolated base archive is checked against Git for every tracked
Go/module input; only identical ignored generated protobuf Go is supplied, after
asserting proto schemas are unchanged. [Manifest](evidence/manifest.json) records
both complete source/API hashes and binary hashes. Historical QG2 evidence stays
unchanged; it is not combined with this new harness's measurements.

The fixture has eight entities, unique entity/timestamp pairs, integer `value=i+1`,
`prior=i`, and float `fv=i+1`, with a shared fixed base timestamp
`2026-10-30T01:00:00Z`. An independent loop computes scalar/group sums;
per-entity COUNT checks all eight entities visible. Full-window totals are 2,080
and 33,558,528. Every measured response is decoded, sorted and checked. Raw records
retain fixture-record hashes, complete parameterized requests, request hashes,
expected strings and oracle hashes. Paired base/head requests and answers are
checked identical by the summarizer. Successful result strings equal the oracle
by assertion, not by treating a database result as the expected value.

[Source/semantic review](source-review.md) records 30 focused tests and generated
QG2 correctness on both real RPC topologies: 12 generated scenarios, 60 successful
and two expected-overflow RPCs per topology. Those semantics checks cover NULL,
empty, masking, overflow, sharing and cleanup beyond this positive-value timing
fixture. Standalone timings do not establish distributed performance.

```sh
python3 verification/qg2bench/p4/run.py build --out /absolute/fresh-output
python3 verification/qg2bench/p4/run.py prepare --out /absolute/fresh-output
# Run only after other builds/tests/profiles are finished.
python3 verification/qg2bench/p4/run.py timing --out /absolute/fresh-output
python3 verification/qg2bench/p4/summarize.py /absolute/fresh-output
```

[Protocol](protocol.md) was frozen before measurement. Five alternating
base/current fresh-process pairs run at each size: three ordinary base arms and
eight current arms, rotating order, 20 warmups, two cache assertions and 100 timed
RPCs/arm. Five additional cached/bypass process pairs use 64-row computed SUM.
There are 30 timing processes plus seven qualification/memory/profile processes.
Every process passed; no successful run was repeated or dropped. One pre-freeze
build was correctly rejected when final lint formatting changed source during
compilation; it produced no measurements. Final source/binary identity matches.

## Fixed default policy and process costs

Observed containers use CPUs 0/1, two-CPU quota, GOMAXPROCS=2, 4 GiB memory and no
additional swap. No native allowance override is passed. Source defaults give a
256 MiB QL tracker and 75%-of-cgroup process protector (3 GiB here), serving
different admission roles. All declared arms are admitted; no admission boundary
or new rejection-recovery claim is made. CPU throttling counters remain zero.
Host one-minute load spans 1.35–3.38; affinity is not exclusive CPU ownership.

Table p50/p95 values are means of five **per-process request quantiles**, with
p50 ranges retained. Each process uses nearest-rank quantiles of 100 durations.
CPU/allocated bytes/objects and GC CPU include client, server and background
work. Request objects are prepared outside timing; transport, server binding,
metadata/cache checks, execution and response formatting/verification are inside.
Forced GC and profiling are outside latency measurements. Raw samples, GC counters,
cache/route counters and observed resources are in [observations](evidence/observations.json)
and [run records](evidence/runs.json).

| Rows | Revision | Arm | Mean process p50 ms [range] | Mean process p95 ms | CPU ms/RPC | Allocated B/RPC | Objects/RPC |
|---:|---|---|---:|---:|---:|---:|---:|
| 64 | base | float_sum | 0.471 [0.400, 0.559] | 0.990 | 0.750 | 164210 | 1016 |
| 64 | base | group_sum | 0.500 [0.474, 0.556] | 0.843 | 0.811 | 148508 | 1473 |
| 64 | base | scalar_sum | 0.428 [0.398, 0.449] | 0.752 | 0.643 | 110323 | 1161 |
| 64 | current | float_coalesce | 0.423 [0.394, 0.446] | 0.998 | 0.812 | 175658 | 1091 |
| 64 | current | float_sum | 0.482 [0.423, 0.658] | 1.031 | 0.836 | 165307 | 1028 |
| 64 | current | group_coalesce | 0.521 [0.477, 0.620] | 0.971 | 0.796 | 158403 | 1517 |
| 64 | current | group_computed | 0.563 [0.466, 0.774] | 1.056 | 0.853 | 165364 | 1552 |
| 64 | current | group_sum | 0.509 [0.450, 0.543] | 0.905 | 0.852 | 147404 | 1461 |
| 64 | current | scalar_coalesce | 0.517 [0.420, 0.693] | 0.868 | 0.797 | 117860 | 1201 |
| 64 | current | scalar_computed | 0.461 [0.398, 0.527] | 0.825 | 0.733 | 122243 | 1224 |
| 64 | current | scalar_sum | 0.474 [0.382, 0.531] | 0.900 | 0.765 | 109303 | 1152 |
| 8192 | base | float_sum | 0.528 [0.397, 0.644] | 0.928 | 0.901 | 139947 | 1069 |
| 8192 | base | group_sum | 2.288 [1.911, 2.687] | 3.885 | 2.906 | 529687 | 1475 |
| 8192 | base | scalar_sum | 1.463 [1.052, 1.976] | 3.121 | 2.104 | 490836 | 1166 |
| 8192 | current | float_coalesce | 0.525 [0.420, 0.564] | 0.975 | 0.947 | 164547 | 1149 |
| 8192 | current | float_sum | 0.520 [0.389, 0.611] | 0.865 | 0.877 | 154266 | 1086 |
| 8192 | current | group_coalesce | 2.236 [1.746, 2.796] | 3.997 | 2.937 | 568078 | 1555 |
| 8192 | current | group_computed | 2.327 [1.856, 2.715] | 4.086 | 3.048 | 639331 | 1597 |
| 8192 | current | group_sum | 2.095 [1.818, 2.238] | 3.875 | 2.753 | 554183 | 1495 |
| 8192 | current | scalar_coalesce | 1.593 [1.063, 2.446] | 2.976 | 2.132 | 521131 | 1233 |
| 8192 | current | scalar_computed | 1.774 [1.206, 2.812] | 3.466 | 2.406 | 593673 | 1265 |
| 8192 | current | scalar_sum | 1.361 [0.979, 1.467] | 2.901 | 1.974 | 516597 | 1186 |


The predeclared primary comparison is ordinary scalar SUM at 8,192 rows.
Head-minus-base paired p50 is **−0.1023 ms**, ratio **0.9493**, with diagnostic
bounds **[−0.3254,+0.1222] ms** and **[0.8220,1.1150]**. At 64 rows the supporting
ordinary scalar change is +0.0460 ms, bounds [−0.0092,+0.0983]. Neither establishes
a repeatable same-query latency regression. No practical tolerance/SLO was
invented and no independent confirmation was triggered. Allocation difference at
8,192 is +25,761 B/RPC on average, driven mostly by one pair (+114,287 B); the
other pairs add about 631–8,641 B. This variable process-wide measurement does not
identify a new allocation defect.

Bootstrap uses 20,000 paired-process resamples, seed 1412, with 5th/95th-percentile
bounds (two-sided 90% interval/equivalent one-sided 95% limits). Requests are not
resampled as independent processes. [All comparisons](evidence/comparisons.json)
retain five individual differences/ratios and supporting CPU/allocation effects.

## Feature work and changed-binding reuse

At 8,192 rows, scalar computed SUM versus direct SUM adds 0.4128 ms process-p50
on average, geometric ratio 1.2658, bounds [1.1089,1.5333], and 77,076 B/RPC.
One pair has a large 1.3601 ms difference; all five are retained. Grouped computed
SUM adds 0.2328 ms and 85,148 B/RPC. These compare different required work;
**they are not same-query regressions or optimization speedups**.

Counters separate the work:

| Current path | Work per RPC |
|---|---|
| Scalar/group computed, 64 rows | 64 input evaluations, one batch, one capacity allocation, 896 cumulative buffer bytes |
| Scalar/group computed, 8,192 rows | 8,192 input evaluations, four–five batches, three capacity allocations, 72,448 cumulative buffer bytes |
| Scalar/float COALESCE | One postaggregate output row evaluated |
| Group COALESCE | Eight postaggregate output rows evaluated |
| Eligible float SUM and float COALESCE | One observed native aggregate execution per RPC, including every timed request |

The three allocations across four–five batches demonstrate existing capacity
reuse. The cumulative buffer counter is not a live peak. Direct typed columns
are already forwarded when compatible, and identical aggregate recipes are
already deduplicated; see the source review. No output/group-construction excess
or duplicate-input defect was identified, so the conditional many-group or
repeated-expression expansion was not triggered.

The cache control alternates first/second half-time windows with equal 32-row
work and different independent answers **976/1104**. Cached and forced-bypass
arms pass both pre-timing and all 100 timed cache-delta assertions per process:
hits/no compiles versus bypass compiles. Mean process p50 is 0.5117/0.5392 ms,
paired delta +0.02746 ms, bounds [−0.00208,+0.06127], ratio 1.0526 with bounds
[0.9946,1.1237]. Latency benefit remains inconclusive. Bypass consistently adds
allocation: mean +21,586 B/RPC (ratio 1.1778) and +0.0783 ms CPU/RPC. This is a
representative compiled-plan reuse test, not a generalized cache guarantee.

## Setup versus expression execution

Separate profiles use 1,000 verified RPCs each: 8,192-row scalar direct/coalesce/
computed and grouped computed, plus changed-binding 64-row scalar computed with
cache/bypass. Allocation profiles subtract warmed before/after snapshots;
CPU profiling surrounds requests. Sample rate is 512 KiB. Raw `.pprof` and flat/
cumulative text are retained under `evidence/profile*`; for example
[computed allocation](evidence/profile-8192-0-current-cache/scalar_computed-alloc-cum.txt)
and [cached setup](evidence/profile_cache-64-0-current-cache/scalar_computed-alloc-cum.txt).

- At 8,192 rows, `expressionNode.buffer` accounts for profile-reported 54.05 MB
  over scalar computed's 1,000 requests (9.21% sampled allocation); operator
  construction `expressionOperator.compile` accounts for 7.01 MB (1.19%).
  Arithmetic kernels account for 5.16% sampled CPU. Most cumulative CPU remains
  under aggregate consumption/source work, not expression compilation.
- Group computed shows 59.76 MB (9.87%) at the buffer owner and 8.01 MB (1.32%)
  at operator construction; arithmetic kernels account for 3.05% sampled CPU.
- At 64 rows, a cache hit still constructs execution-local operators: compile
  accounts for 5.51 MB in the cached profile versus 6.01 MB with bypass.
  `ResolveExpression` appears in both. This is binding/execution setup, **not a
  compiled-plan cache miss**. Bypass additionally attributes 24.06 MB (16.62%)
  to `compiledPlanCache.uncached`, consistent with observed extra compiles.

Profile cumulative owners overlap and cannot be added. Allocation snapshots also
include profiler serialization/start/stop and background activity; these stacks
are retained rather than labeled server-only. Even 1,000 small requests provide
only 610/710 ms sampled CPU in cached/bypass profiles, so small setup CPU shares
remain coarse. Exact compiler-only CPU/bytes and per-owner live peaks are
[null where uninstrumented](evidence/missing-counters.json). The measured buffer
and execution-local setup work has an owner, but this evidence does not establish
that it can be removed while preserving binding, masking, overflow and ownership.

## Single-query memory and cleanup

Each isolated probe warms the query, requires idle ownership, forces GC, asserts
peak reset succeeds, then executes **one** verified RPC. After 20 ms quiet and GC,
query reservations/results, scan cursor/pins/results/reservations/tasks,
cache in-flight/compile/uncached state, and expression/native LiveBytes are zero.
Standing decode workers and retained cache entries remain intentional process
infrastructure. Heap deltas are separate observations, not leak assertions.

| Rows | Arm | Single-query tracked peak B | Post-GC retained heap delta B |
|---:|---|---:|---:|
| 64 | float_sum | 1225306 | -590312 |
| 64 | scalar_sum | 1928622 | -36576 |
| 64 | group_sum | 2121090 | -48160 |
| 64 | float_coalesce | 1242858 | -42528 |
| 64 | scalar_coalesce | 1943566 | -46488 |
| 64 | scalar_computed | 1954318 | 361848 |
| 64 | group_coalesce | 2141666 | -430200 |
| 64 | group_computed | 2157026 | 4328 |
| 8192 | float_sum | 5719980 | -1922560 |
| 8192 | scalar_sum | 40442540 | -737472 |
| 8192 | group_sum | 44699552 | -73664 |
| 8192 | float_coalesce | 5737532 | -424080 |
| 8192 | scalar_coalesce | 40457484 | -723672 |
| 8192 | scalar_computed | 40530188 | -554856 |
| 8192 | group_coalesce | 44720128 | -775864 |
| 8192 | group_computed | 44797288 | -219328 |

At 8,192 rows scalar direct/computed peaks are 40.44/40.53 MB tracked reservation;
the additional computation is a small part of total admission pressure. These
are not heap/RSS peaks, and cumulative lease/owner charges must not be treated as
live memory. Post-GC deltas include pools, cache state and collection timing;
positive small-fixture deltas do not alone prove a leak, nor do negative ones
prove leak freedom. Memory-pass allocation deltas include cleanup instrumentation;
use unprofiled timing B/RPC for churn. Whole-container peaks across all processes
span 18.1–53.1 MiB, including fixture/index/runtime/profiles/page cache, not
per-query RSS. Long-lived retention and maximum capacity remain unmeasured.

Final scoped lint, Python compilation, portable summarization, source/binary
identity and artifact hashes pass. Verdict: **bounded costs and ownership
verified; baseline latency decision inconclusive; no confirmed removable work
or production change**. The next planned investigation is P5 exact decimals.
