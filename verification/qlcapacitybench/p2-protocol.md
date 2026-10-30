# P2 fixed capacity protocol

Declared before timing, 2026-10-29. Production revision is the separately recorded
P1 successor `5e78df39d`; no production optimization belongs to this campaign.

The public seam is `BydbQLService.Query`. First run the generated QL-P2 cohort
on standalone and two-worker distributed topology, including a repeated service
on normally routed different entities. Every returned aggregate must match its
independent fixture oracle. Actual configured hot/warm/cold migrations are a
separate correctness checkpoint; a reproducible lifecycle defect stops that
checkpoint without authorizing lifecycle changes.

Capacity holds R=50,000 rows, G=8 groups and W=16 service-key bytes while increasing
D (distinct group/key entries) through 1,000, 10,000 and 50,000. One rejection is
repeated exactly once to establish the boundary; further D sizes are skipped.
At D=1,000, independently vary R=10,000/100,000, G=1/64, and W=256 (other dimensions
stay at their control values). Each group is a routing entity; the service key
varies across distinct millisecond timestamps. One key belongs to one group, so
D is the total distinct state, rather than D per group. The oracle enumerates
the declared distinct key domain independently of QL execution.

Each fixture first qualifies total source row visibility through QL COUNT. Every
capacity query returns the complete ordered grouped COUNT DISTINCT service, checked structurally against the independent oracle on every call.
A wrong result or non-admission error stops that run; it is never a latency sample.
All arm sizes are fixed before feasibility. No arbitrary resource-limit increases,
spilling, approximate results or dropped keys are permitted.

Use the frozen Go1.26.10 Docker image, two CPUs, four GiB RAM, no extra swap,
GOMAXPROCS=2, existing 256-MiB vectorized tracker and default native/protector
policy (96-MiB native allowance in that cgroup). Integration topology resources
follow P1: standalone2CPU4GiB/distributed4CPU8GiB with its explicit128-MiB native
allowance. Three-tier real-stage checkpoint uses4CPU8GiB; it is correctness only.
Record image, source, runner, fixture, request, oracle and binary digests and
observed cgroup settings. Existing P1 evidence remains immutable.

For each admitted capacity arm only, run five fresh processes, three warmups and
30 timed successful RPCs per process. Rotate complete arm order by process index.
The primary reported metric is process p50 RPC latency; also report process p95,
CPU and allocated bytes per RPC. This is a descriptive capacity curve with no
before/after improvement claim or invented noninferiority gate. Every timed RPC
retains full result checking. A rejected or incorrect request stops that timed
cohort; do not lower counts, adapt repetitions or replace failed samples.

Isolated memory probes reset peaks only when ownership is idle, record tracked
peak/cumulative lease charge and native row/frame counts, and require20ms quiet
zero-live ownership. Memory-only before/after allocation profiles use4KiB sampling
and explicit GC outside latency intervals. Profile stack owners separately from
conservative reservations. No build/test/profile runs overlap timing.

Finally cancel after native work is underway, require cleanup and same-cluster
recovery, and launch eight callers at a size already admitted for one caller.
Record individual successes/rejections and cleanup, then recover on that cluster.
If no size is admitted, explicitly skip the load probe. No rejected latency is
reported as successful-query speed. Missing owner-residency measurements remain
null with limitations; observed aggregate tracked peak is not per-owner heap.

Before any timing, the initial full integration run exposed the expected standalone
SUM timestamp-frontier boundary:40 correct RPCs and8 ResourceExhausted outcomes.
The successor primary capacity shape is COUNT DISTINCT only with complete group
ordering, retaining the identical R/D/G/W fixtures. Each process first verifies
the generated small combined control (16 positive rows, SUM16, entities8,
services2). The original full integration qualification remains failed. The
runner's explicit --allow-standalone-admission-gap option permits independent
capacity timing only after the two-worker cohort passes and all three standalone
spec failures contain exclusively the expected admission assertion with exactly
48 attempts/8 ResourceExhausted records. This is a declared prerequisite exception
before timing, not relabeling rejected calls as correct or lowering admission.

The isolated memory pass also replays the generated full combined query expected
to exhaust the P1 fixture's native timestamp frontier, requires zero live query/
cache/native cursor ownership, then replays the admitted generated combined
control on that same cluster. This covers rejection cleanup even if the bounded
count-only D sweep reaches50,000 entries without a rejection.
