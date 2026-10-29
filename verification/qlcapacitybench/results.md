# P1 measured result — 2026-10-29

**Verdict: measured cost growth and a native admission boundary; no production
regression, incorrect result, resource ownership defect or optimization is claimed.**
Q1/A14 were investigated on PR #1447 head
`efc0c18988ecbc18a10ffbdd49c7491299d8c8e7`. Production code is unchanged.

## Correctness and capacity

The independent fixture has 10,240 rows, 4,096 active distinct entities and 8,192
positive rows. Every service was checked through indexed QL pages: five entities,
ten rows, value sum eight, count sum ten; its positive subset has four entities
and eight rows. `storage-only.entity_id` remains unindexed. Two shards have no
replicas or explicit sharding key. Group/schema/index/row digest is
`cde02cbfca428d7d4069cbbba68f48af6fbce6c93da7c9c8fab50e3b7df650c8`.

| Real QL RPC cohort | Specs | Verification RPCs | Readiness RPCs | Exit |
|---|---:|---:|---:|---:|
| Standalone, 2 CPUs / 4 GiB | 4 failed capacity qualification | 12 correct, 32 ResourceExhausted | 64 | 1 |
| Distributed, 4 CPUs / 8 GiB, two workers | 4 passed | 44 correct | 64 | 0 |

Distributed traces prove both workers returned data, for example 146 + 174 = 320
source rows for the first page; every page totals 320. Complete IN/NOT IN sizes,
seven/zero present IDs, duplicates, typed empty arrays/NULL, CASE 2/16 branches,
changed memberships, overlap/first match, and both cache modes passed there.
Results include IN 32/28/0, NOT IN 4,064, CASE 1,024/3,072, swapped 3,072/1,024,
and overlap 1,028/3,068. No query response supplies an expected answer.

Standalone's twelve successful calls are the six nonempty IN bindings in both
cache modes. NOT IN, CASE, and empty/NULL-only IN bindings fail before full
selection can complete. The opt-in qualification retains these failures; it does
not rewrite them into success expectations or make the ordinary CI suite fail.
All 44 verification calls were attempted, including after admission failures.

Focused standalone feasibility used the default process protector derived from
its four-GiB cgroup (75% = three GiB), distinct from the explicit 256-MiB vectorized
query tracker. The native protector pool is allowance/4 and its per-query maximum
is pool/8: **96 MiB** here. Existing vectorized integration suites explicitly use a
four-GiB process allowance, giving a 128-MiB native maximum per node. Neither
setting was raised for this experiment. Error identity/source is the existing
native protector query budget, `interrupt: hit 0: query memory pool exhausted`.
This is admission rejection, not an OOM kill or proof of a vectorized-tracker defect.

| Standalone arm | IDs / raw ID bytes | Protobuf request bytes | Feasibility |
|---|---:|---:|---|
| IN | 10 / 120 | 299 | 2/2 correct |
| IN | 1,000 / 12,000 | 14,159 | 2/2 correct |
| IN | 10,000 / 120,000 | 140,161 | 2/2 correct |
| NOT IN | 10 / 120 | 303 | 2/2 rejected |
| NOT IN | 1,000 / 12,000 | 14,163 | 2/2 rejected |
| NOT IN | 10,000 / 120,000 | 140,165 | 2/2 rejected |
| CASE 2 | 10,000 / 120,000 | 140,296 | 2/2 rejected |
| CASE 16 | 10,000 / 120,000 | 141,008 | 2/2 rejected |
| IN 10, bypass | 10 / 120 | 299 | 2/2 correct |
| CASE 2, bypass | 10,000 / 120,000 | 140,296 | 2/2 rejected |

All six rejected arms recovered with a correct small-IN query on the same
cluster, after zero-live query/native ownership assertions. No rejected arm was
timed. The tested admitted list envelope is 10,000 IDs / 120,000 raw ID bytes;
this is not a global maximum. NOT IN already rejects at ten IDs because its
broader native source exceeds admission; list length alone does not explain it.
CASE branch cost and standalone cache cost remain unqualified at this capacity.
Distributed correctness is not a distributed performance measurement.

## Five fresh-process rounds

Each admitted arm completed **20 warmups + 200 timed successful RPCs in each of
five fresh processes**: 4,000 timed RPCs, 400 warmups and 40 separate cache-check
RPCs. Result checking is identical for every arm. Corresponding list-size arms
alternate the same two eight-service cohorts (0000–0007 and 0008–0015); each reads
exactly **80 native input rows per RPC** and returns 32. This keeps selected work
fixed across list sizes while exercising changed bindings. Generated RPC witnesses
independently check changed answers 32/28/0 under both cache modes. Separate timed-
process probes assert two hits/no compile, or two bypass compiles, outside timing.

Numbers below are **means of five per-process p50/p95 values**, not pooled RPC
quantiles. CPU, allocations and GC include client/server/background work in this
one process; protobuf parameter construction precedes timing, while transport,
server binding/compilation/evaluation and identical result checking are included.
No forced GC occurs in latency loops.

| Arm | Mean process p50 ms | Mean process p95 ms | CPU ms/RPC | Allocated bytes/RPC |
|---|---:|---:|---:|---:|
| IN 10 | 34.932 | 37.830 | 37.202 | 4,135,806 |
| IN 1,000 | 36.771 | 39.837 | 40.031 | 6,699,321 |
| IN 10,000 | 54.974 | 60.641 | 66.181 | 31,328,008 |
| IN 10 bypass | 35.177 | 37.810 | 37.496 | 4,165,522 |

Primary descriptive contrast, IN 10,000 versus IN 10, is +20.041 ms in mean
process p50 (paired-process bootstrap 95% interval +19.618 to +20.480 ms), ratio
1.574 (1.561–1.587), with +27,192,202 allocated bytes/RPC. This is a cost curve
across different list sizes, not a same-query regression or an SLO decision.
The small bypass p50 difference is +0.245 ms (0.057–0.463); p95 is -0.020 ms
(-0.353–0.292). No causal cache-speedup or non-inferiority conclusion is drawn.

Order rotates the full ten-arm list before rejected arms are filtered: round 0
is 10/1k/10k/bypass, round 1 is 10k/bypass/10/1k, rounds 2–4 are
bypass/10/1k/10k. This is not a balanced Latin-square design. Raw/per-round values,
CPU/allocation/GC counters and paired effects are in `results/summary.json` and
`results/round-*.jsonl`; bootstrap resamples paired processes, never individual
RPCs. There is no invented percentage gate or adaptive confirmation campaign.

Observed timed cgroups have `cpu.max=200000 100000`, memory.max=4,294,967,296,
memory.swap.max=0, zero throttled periods and zero OOM events. Recorded cgroup
memory peaks are 102.3–116.4 MB. Docker distributed limits are four CPUs/eight GiB,
with memory-swap equal to memory. Host load and observed resource settings are
preserved; CPU quota does not provide exclusive host ownership.

## Memory, cleanup and bounded concurrency

An isolated process warmed admitted arms before their idle/post-GC baselines,
asserted zero live ownership before resetting query peak, executed one query,
then asserted zero live query/scan ownership and compared named pool references.
Per-query lease-charge deltas are separate from peak reservations. Bypassed arms
actually use the bypass hook and record cache counters.

| Isolated arm | Peak tracked bytes | Post-GC process heap delta bytes |
|---|---:|---:|
| IN 10 | 8,382,077 | -8,392,504 |
| IN 1,000 | 10,963,997 | -1,976 |
| IN 10,000 | 34,435,997 | +376 |
| IN 10 bypass | 8,382,077 | -32,456 |
| Rejected NOT IN / CASE arms | 99,760,341–99,886,437 | +3,438,840 to +4,635,448 |

Every observed live reservation returns to zero. Native scan snapshots are idle:
no cursor/result references, owner pins, scan reserved bytes or decode tasks.
Named query/cursor pool references return to baseline. Persistent decode workers
are expected. Native protector reservation counters are private; these assertions
and successful same-cluster recovery are evidence of cleanup, not direct access
to that counter or proof of leak-free physical heap. Rejected arms' retained-heap
deltas remain unattributed and include cache/pool/background effects. The first
negative delta demonstrates baseline drift; no operator-retention claim follows.

The cancellation probe observed a native leaf underway on admitted IN 10,000,
received `Canceled`, asserted cleanup and recovered on the same cluster. Its
reported historical peak was not reset for cancellation and is not an isolated
cancellation peak. A separate eight-caller IN 10,000 probe returned 8/8 correct,
zero rejections, a **process-aggregate** tracked peak of 87,532,063 bytes, zero
remaining ownership and successful small-IN recovery. It supplies no throughput
SLO or high-concurrency envelope.

## Diagnostic ownership evidence and remaining work

One extra profiled process used the identical frozen binary and only IN 10 /
IN 10,000, with default Go allocation sampling (512 KiB), after timing/memory.
Its setup, writes, 64 readiness queries, warmups, both arms, client and background
work are included. Profiled RPCs are excluded from the latency table. Mixed CPU
samples total 29.49 s: native series-index search is 60.60% cumulative,
`MatchTermsSet` 56.63%; expression membership is 3.73% cumulative. Sampled
allocation hot spots are expression-node `compile` (32.04% flat),
`ResolveExpression` (11.76%) and `appendBoundExpressionArgument` (6.46%).

This confirms allocation hot spots in runtime expression construction/resolution/
binding alongside native index CPU cost. The combined profile does not isolate
how much of the size contrast belongs to each owner, prove unnecessary work, or
establish a same-query regression. A bounded follow-up could investigate a compact
per-execution representation of constant IN bindings while preserving indexed
pruning, NULL/NOT IN, first-match semantics, immutable cached recipes and native
tracking. No implementation, speedup claim, budget weakening or CASE optimization
is authorized by these observations.

## Reproduction and checks

Use the commands in `README.md`. `results/manifest.json` freezes benchmark source,
runner, binary, Go 1.26.10, image ID, seed and protocol. The correctness manifest
is retained separately: only the benchmark gained the bounded eight-caller probe
before measurement freeze; standalone/distributed binary hashes are identical in
both manifests. All source/binary hashes, QL/binding/result digests, raw samples,
process profiles, compact topology outcomes and exact commands/exits are retained
under `results/`; large test logs/binaries remain at `/tmp/ql-p1-final` locally.

Validation completed without committing or publishing:

- `go test -tags=querymetrics ./test/cases/measure/qlgen ./test/cases/measure/data ./verification/qlcapacitybench -count=1`: exit 0.
- Focused harness checks after the final probe addition: exit 0; wrong-result and changed-oracle sensitivity checks pass.
- `generate-ql --check`: exit 0; deterministic artifact retained in existing canonical pretty JSON format (4.44 MB added).
- Scoped `golangci-lint` with querymetrics: exit 0, zero issues; configured gci/gofumpt formatting applied.
- Repository license check: exit 0, 5,424 checked, zero invalid; final new-source scoped check recorded separately.
- Both real topologies: exits 1/0 as above, nonzero spec counts and exact focused RPC counts retained.
- Feasibility, all five timed processes, memory/cancellation/eight-callers and diagnostic profile: exit 0.

P1's standalone full-cohort capacity qualification is **not passed**. CASE and
NOT IN standalone cost curves remain unavailable under the fixed policy, retained
heap attribution is unresolved, and no production optimization has been made.
P2–P6, stage replay, QG2/QG3 replay and historical campaigns were not expanded.
