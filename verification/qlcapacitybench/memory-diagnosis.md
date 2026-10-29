# P1 rejection memory diagnosis — 2026-10-29

**The reported 3–5 MB retention is mainly reusable pooled scan and transport
memory, and does not accumulate across repeated rejected queries. The admission
failure comes from the native scan's wide timestamp-ordered merge frontier before
the DISTINCT operator receives its first batch.** No change to the memory guard,
source ordering, reservation bounds, or existing pools is warranted by this test.

This diagnosis used an isolated detached worktree at
`efc0c18988ecbc18a10ffbdd49c7491299d8c8e7`, with the existing P1 fixture/harness
copied in. It does not measure the concurrent membership optimization. The fixture
has 10,240 rows in 5,120 series, with two coincident timestamps per series.
Standalone resources remain two CPUs / four GiB, no swap, default native protector,
and a separate 256-MiB vectorized query limit. Observed cgroup values are retained
in [the raw observations](memory-diagnosis/retention.jsonl).

## Repeated rejection and ownership

The final experiment warmed each template/error path, then repeated the identical
NOT IN 10 and CASE-16 requests sixteen times each. Every rejection was followed by
a successful small IN query on the same cluster. It also included five idle
checkpoints and five admitted-query controls. Client request arrays remain live
through the final idle control, avoiding a false heap drop when the harness stops
using its own parameters. Forced GCs and profiles are outside any latency test.

- **34 expected ResourceExhausted responses**, including two warmups.
- **42 correct admitted/recovery responses**, in addition to fixture readiness.
- Every ownership assertion passed: native scan idle, vectorized live reserved
  bytes zero, no live results/owner pins/in-flight compilation, and named native
  query pool references restored to baseline.
- The native pool is already tracked: `measure-blockCursor` uses `pkg/pool.Synced`.
  A returned object can remain reusable in the underlying `sync.Pool` after its
  checked-out reference count reaches zero.

| Identical rejected query | Heap after one GC, range | Heap after two GCs, range | First-to-last change after two GCs |
|---|---:|---:|---:|
| NOT IN, 10 IDs | 34,636,040–34,653,896 B | 31,214,968–31,216,296 B | +1,040 B |
| CASE, 16 branches / 10,000 IDs | 35,837,024–35,895,824 B | 31,266,392–31,289,248 B | +21,952 B |

The five final idle observations are identical: **31,280,328 B**. Plan-cache bytes
stay constant throughout each repeated arm: 45,777 B for NOT IN and 98,064 B after
CASE is warmed. The initial independent replay also showed the same one-GC versus
two-GC separation. This is bounded evidence against a cumulative query leak in
these cases, not a general proof that every workload is leak-free.

The [NOT IN profile](memory-diagnosis/not-in-pooled.txt) attributes about 62% of
the first-GC incremental sampled heap to block-cursor objects; cursor metadata,
decoded columns and pool bookkeeping account for most of the rest. The
[CASE profile](memory-diagnosis/case-pooled.txt) additionally attributes about 25%
to gRPC sized-buffer pools. These objects largely disappear after a second GC.
The remaining [CASE repeat delta](memory-diagnosis/case-repeat-retained.txt) samples
metrics labels, JSON diagnostic output, and small pool/decode bookkeeping; the
[NOT IN repeat delta](memory-diagnosis/not-in-repeat-retained.txt) is near zero.
Profiles use a 4-KiB sample rate and are estimates; exact heap totals above come
from `runtime.MemStats`. No new pool or pool-clearing production code was added.

## Why the native guard rejects

A separate, short probe recorded the reservation owner and rejection stack in the
same resource envelope. All temporary production probes were then removed.
[Probe values and stack](memory-diagnosis/capacity-probe.txt) and the exact
[temporary patch/command](memory-diagnosis/capacity-probe-manifest.json) are saved.

1. `applyMeasureQueryOrdering` defaults the native source to ascending timestamp
   order. Although the final query aggregates, this source still preserves that
   ordering contract.
2. All 5,120 series overlap in time. `planUnits` therefore creates **one merge
   unit containing 5,120 windows**. To establish the first timestamp-ordered output,
   `timeMerge.fill` opens the overlapping cursors and keeps them active.
3. Shared scan workspace is **4,325,376 B**. Each tiny cursor reserves **34,656 B**:
   `max(2, 16) × (7 × 256 + 256) + 118 × 16`. This is the existing conservative
   decoded-memory bound, not a measurement of the cursor's physical heap.
4. The default native allowance is **100,663,296 B (96 MiB)** for one query in this
   container. The next cursor cannot be reserved before the first batch emerges.

| Probe | Active cursors at rejection | Shared tracker live bytes | Native lease charged / limit | Next requested reservation |
|---|---:|---:|---:|---:|
| NOT IN 10 | 2,307 | 99,614,093 | 100,662,925 / 100,663,296 | 34,656 B |
| CASE-16 | 1,550 | 99,582,537 | 100,640,841 / 100,663,296 | 34,656 B |

CASE reaches the limit with fewer active cursors because its bound expressions
also consume query memory. At rejection the shared tracker is almost as large as
the native lease, apart from the fixed native overhead and small earlier charges:
the main issue here is simultaneously owned frontier reservations, rather than
charges accumulating across already-released batches. The stack ends in
`timeMerge.fill → prefetcher.take/pump → scanBudget.tryReserve → MemoryTracker.Reserve
→ QueryReservation.Charge`. DISTINCT has not received source rows yet.

The experiment identifies a capacity boundary; it does not establish that the
general cursor bound is incorrect. Raw timestamp ordering and all resource limits
remain intact. An unordered source for order-insensitive aggregates would require
an explicit planner/execution contract and is outside this membership fix.

## Reproduction and checks

The opt-in test is [diagnosis_test.go](diagnosis_test.go). Build it alongside the
existing P1 harness, then run the single test in the same image/resource envelope:

```sh
go test -c -tags=querymetrics -o /tmp/p1-diagnosis.test ./verification/qlcapacitybench
docker run --rm --cpus=2 --memory=4g --memory-swap=4g \
  -e GOMAXPROCS=2 -e P1_DIAG_OUTPUT=/evidence \
  -v "$PWD:$PWD:ro" -v /tmp/p1-diagnosis.test:/diagnosis.test:ro \
  -v /tmp/p1-memory-evidence:/evidence -w "$PWD/verification/qlcapacitybench" \
  banyandb-distributed-querybench:go1.26.10 /diagnosis.test \
  -test.run='^TestP1RepeatedRejectionMemory$' -test.v -test.timeout=15m
```

The final run passed in 17.11 seconds; scoped `golangci-lint` passed with zero
issues. The exact command, binary/source hashes, image ID, observed Docker limits
and source-formatting note are in [manifest.json](memory-diagnosis/manifest.json).
There were no OOM events or throttled CPU periods. This diagnostic run is not a
latency comparison and may overlap other agents' unit-test work.

Compact raw observations, profile summaries and probe evidence are preserved here.
Full heap profiles and test binaries remain locally under
`/tmp/ql-p1-memory-diagnosis`; their hashes are recorded in
[local-artifact-inventory.json](memory-diagnosis/local-artifact-inventory.json).
The detached baseline has no changes under `banyand`, `pkg`, or tracked `api` files.
Only P1 test/generator/harness files differ from HEAD. Nothing was committed or
pushed, and the original P1 results were not overwritten.
