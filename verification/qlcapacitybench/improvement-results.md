# P1 membership fixes and memory diagnosis — 2026-10-29

This follow-up implements two changes justified by the original
[P1 observations](results.md), and completes the rejection-memory diagnosis.
At 10,000 IDs, the same-query comparison reduces median latency by 25.4%, CPU
by 32.2% and allocated bytes by 72.2%. Small-query controls show modest latency
increases, recorded below; this is not a universal speedup claim.
The baseline evidence in `results/` is unchanged. Changes are local and
uncommitted on `verify/measure-ql-p1`, based on
`efc0c18988ecbc18a10ffbdd49c7491299d8c8e7`.

## What changed

**Proven empty input avoids a native scan.** After full binding and type
validation, a conservative predicate proof marks the existing raw plan empty
when its filter cannot select any row. This includes empty or NULL-only `IN`,
safe rejecting AND branches, both rejecting OR branches, and `NOT IN` containing
an explicit NULL. Empty `NOT IN` remains TRUE. Arithmetic, casts and conditional
expressions exclude the proof so that it cannot hide row-dependent errors.

The existing empty-input pipeline still performs aggregation, HAVING and result
shaping: scalar COUNT returns zero, SUM returns NULL, and grouped input produces
no groups. The shallow execution-plan copy drops only the WHERE filter, whose
hidden source columns are absent from the synthetic empty schema. It does not
mutate the cached template. RPC tests and generated cases exercise empty/NULL
followed by nonempty rebinding.

**Bound string lists stay compact.** A bound string array remains one literal,
instead of expanding into one expression node per ID. Native positive-IN index
criteria consume that array directly. Constant string membership uses a small
linear lookup for at most 16 values and an execution-owned hash lookup above
that size. It does not construct a vector evaluator for each RHS value.

Lookup memory is reserved before allocation and construction checks cancellation.
The reservation includes conservative map overhead; string payloads are borrowed
from the execution's immutable literals. Closing the expression operator drops
the lookup with its nodes and releases reservations. Dynamic RHS expressions
retain their evaluation order, and numeric lists retain existing coercion and
comparison rules. NULLs, duplicates and CASE first-match semantics are unchanged.

The original failing reproducer bound 10,000 string IDs against six rows with a
2-MiB query budget. It requested a 10,563,136-byte expression clone reservation
before execution. The compact representation completes under the same budget;
a separate 256-KiB test still rejects lookup allocation cleanly. Cache rebinding
and mutation of the caller's array after binding are tested.

Large literal lists written directly in QL still have parser/template nodes;
this change principally targets the recurring parameter-array workload.

## Generated QL RPC correctness

The current generated cohort contains 48 requests per topology, including four
new calls that rebind nonempty lists after empty/NULL bindings. Both cache-hit and
cache-bypass modes execute every case. All admitted results match the independent
fixture oracle.

| Topology | Correct responses | Native-budget rejections | Spec outcome |
|---|---:|---:|---|
| Standalone, 2 CPUs / 4 GiB | 20 | 28 | IN passes; NOT IN and both CASE specs retain capacity failures |
| Distributed, 4 CPUs / 8 GiB, two workers | 48 | 0 | All four specs pass |

Compared with the original 44-call standalone cohort, the four empty/NULL IN
calls and two NULL-only NOT IN calls now return correctly. Two additional
nonempty IN rebinds pass, while two additional broad NOT IN rebinds hit the
existing capacity limit. Consequently 12 correct / 32 rejected becomes
20 correct / 28 rejected across the expanded cohort. These are not equal-sized
denominators. The standalone qualification command intentionally exits nonzero;
capacity rejection has not been turned into a passing assertion.

## Same-query comparison protocol

The frozen original benchmark binary is compared with the candidate in five
pairs of fresh processes. Baseline runs first in pairs 0/2/4 and candidate first
in 1/3. Both use identical arm order, two CPUs, four GiB, no extra swap,
GOMAXPROCS=2, the same image, schema, fixture, query text and bindings. Each process
runs 20 warmups and 200 successful timed RPCs for IN 10/1,000/10,000 and IN 10
with cache bypass: 8,000 timed RPCs total across the two versions. No builds,
unit tests, profiles or forced GCs run during timing.

The two parameter cohorts select the same eight services at every list size;
each query returns 32 distinct entities from 80 native input rows. The comparison
checks equal query/result digests, request/list bytes and 16,000 native source rows
per timed arm. Latency summaries average the five process-level quantiles;
uncertainty intervals resample process pairs, never individual RPCs. There is no
post-hoc percentage gate. Rejected NOT IN/CASE queries are not latency samples.

## Measured changes

All 8,000 timed RPCs succeeded. Figures below are means of five process summaries;
MB denotes decimal millions of bytes. CPU and allocations cover the entire
client/server process, including background work.

| Query | p50, before → after (ms) | p95, before → after (ms) | CPU/RPC, before → after (ms) | Allocated MB/RPC, before → after |
|---|---:|---:|---:|---:|
| IN 10 | 35.524 → 35.710 | 38.136 → 38.665 | 37.971 → 38.208 | 4.132 → 4.114 |
| IN 1,000 | 37.570 → 36.270 | 40.779 → 39.065 | 41.147 → 38.742 | 6.717 → 4.530 |
| IN 10,000 | 56.240 → 41.946 | 62.007 → 45.526 | 68.027 → 46.096 | 31.395 → 8.729 |
| IN 10, cache bypass | 35.535 → 35.993 | 38.428 → 38.838 | 37.990 → 38.499 | 4.165 → 4.142 |

The large-list improvement appears in all five pairs. Its mean paired p50
reduction is **14.294 ms**, with a process-pair bootstrap 95% interval of
13.970–14.625 ms; the candidate/baseline ratio interval is 0.742–0.750.
Allocated objects fall from 205,450 to 125,371 per RPC. At 1,000 IDs, p50 falls
1.301 ms (interval 1.016–1.664 ms), and allocated bytes fall 32.6%.

The small controls must remain visible: IN 10 p50 increases **0.187 ms (0.53%)**,
interval +0.079 to +0.298 ms; bypass p50 increases **0.458 ms (1.29%)**, interval
+0.217 to +0.721 ms. Their CPU increases are 0.237 and 0.509 ms/RPC. This campaign
does not establish the cause of those small increases, and they are not dismissed
as noise or called a pass under an invented tolerance. Five process pairs are a
limited local experiment, not a production SLO guarantee. No additional rounds
were selected to change the result.

The mechanism matches the large-list measurements: cached templates previously
expanded a fresh bound array into thousands of expression nodes on every call.
Compact binding removes that construction, cloning and evaluator allocation;
the execution-owned set also avoids repeated long-list comparisons per row.
Native index pruning and the 80-row selected cohort remain identical. The
remaining 8.729 MB is total RPC/process allocation, not membership-only memory.

### Memory and lifecycle checks

The isolated candidate memory run passes all cleanup assertions after successful,
rejected and canceled requests, and after eight concurrent requests. Native scan
cursor references, owner pins, result leases, decode tasks and reservations return
to zero, as do live vectorized reservations and named checked-out pool references.
An underway 10,000-ID query returns Canceled, then a small query succeeds on the
same cluster. All eight simultaneous 10,000-ID requests return the correct result,
followed by a successful recovery query.

| Query | Original peak tracked reservation | Candidate peak tracked reservation |
|---|---:|---:|
| IN 10 | 8,382,077 B | 8,357,229 B |
| IN 1,000 | 10,963,997 B | 8,373,069 B |
| IN 10,000 | 34,435,997 B | 8,517,069 B |
| Eight concurrent IN 10,000 | 87,532,063 B | 27,683,274 B |

The single-query large-list peak is 75.3% lower. These are conservative tracked
reservations from isolated probes, not measured physical heap. The concurrent
peak aggregates overlapping queries and depends on scheduling; it is not a
per-query limit. Original peaks come from the preserved P1 memory phase, while
candidate peaks come from its identical follow-up phase. These are single probes,
not five-pair statistical estimates. Candidate IN 1,000/10,000 post-GC heap changes
are -1,864/-2,344 B within each warmed probe; no retained growth was observed there.

## Diagnosis and remaining capacity boundary

The [completed memory diagnosis](memory-diagnosis.md) found that the 3–5 MB
retained after one GC belongs mainly to existing tracked native cursor pools and
gRPC buffers. Sixteen repeated failures changed twice-GC heap by only +1,040 B
for NOT IN and +21,952 B for CASE; final idle heap was flat. It did not reproduce
a cumulative query leak. Adding another pool or clearing useful existing pools
is not supported by this evidence.

Native admission fails before DISTINCT receives a row. The timestamp-ordered
scan must open an overlapping frontier of 5,120 series, reserving 34,656 bytes
per cursor plus shared workspace. The focused container's native per-query
allowance is 96 MiB, distinct from the 256-MiB vectorized tracker. Compact
membership removes expression overhead but cannot make this broad ordered
scan fit. The integration suite's explicit native allowance is 128 MiB; its
single worker still rejects the broad cohort, whereas two distributed workers
complete it.

No protobuf, native ordering, consistency, memory-limit or spilling change is
included. A source that does not require timestamp ordering for order-insensitive
aggregates would need its own planner/execution contract. This report establishes
the cause without silently weakening the current contract.

## Verification and evidence

The full query-expression, QL planner/binder and liaison unit packages pass.
Focused membership/empty-input race tests pass, as do the P1 generator/harness
tests, generated-case drift check and scoped lint (zero issues). Formatting was
checked without rewriting files. The generated QL RPC outcome and capacity
failures are reported separately above; they are not hidden by these unit checks.

The license check initially flagged saved raw profiler output. Two narrow
`paths-ignore` entries in `.licenserc.yaml` follow the existing convention for
captured benchmark output, preserving the original evidence bytes. Source code
remains subject to the license check; the full rerun passed with zero invalid
files. Full `make check` was not invoked: its
format target performs repository-wide rewriting/staging and expects a clean
committed tree; no commit was requested. The affected checks were run directly.

- [Comparison and uncertainty](improvement-results/comparison.json), including
  all five pairs for every metric.
- [Comparison manifest](improvement-results/comparison-manifest.json), including
  binary identities, pair order, toolchain/image and runner identity.
- [Correctness summary](improvement-results/correctness-summary.json) and exact
  per-spec evidence in the candidate standalone/distributed run records.
- [Candidate memory observations](improvement-results/candidate/memory-0.jsonl)
  and [feasibility observations](improvement-results/candidate/feasibility-0.jsonl).
- [Executed checks](improvement-results/checks.json) and
  [artifact inventory](improvement-results/inventory.json).

All ten timing containers were observed with two CPUs, four GiB and no extra
swap. The comparison validates equal work, and frozen identity checks verify that
runtime/test source, generated cases, binaries, Go version and image did not
change during execution. The manifest's source hash covers Go files, module
files, the generated suite and the runner; documentation/license configuration
edits do not change the measured binary.

Baseline source SHA-256:
`7865d6f40185b1933e74bac658c97c5034a409f21a9e0e62daef94e1098c2b12`.
Candidate source SHA-256:
`5c80d0f110a3ffd918fbaf4d0e985f97b3e9eebdc597c154ccfb15d8da1d3975`.
Go is 1.26.10; the image ID is
`sha256:09c665d7ce2302ef883da73a11f72161ee18a66336dfb7d03f52fd817875d877`.

Full logs and frozen binaries remain under `/tmp/ql-p1-final`,
`/tmp/ql-p1-fixed` and `/tmp/ql-p1-comparison`. Compact raw samples and run records
are preserved under `improvement-results/`. The current 48-call generated
correctness cohort adds rebinding cases; the timed benchmark's query/cohort
protocol is unchanged from the original binary. Nothing was committed or pushed.
