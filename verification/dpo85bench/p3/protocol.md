# P3 current-source protocol

Declared before timing. Run `python3 verification/dpo85bench/p3/run.py build`,
then `run` after other validation finishes, then `summarize` if needed. The runner
refuses repeated logs and changed source/binary identities. No timing threshold,
selective retries, historical binary comparison, or discarded successful rounds.

Two fixture sizes (1,000 and 10,000 whole-value indexed names), two timestamps per
name, eight independently selected matching names. Seven arms: LIKE `%MaTcH%`,
ILIKE `%match%`, equivalent precomputed IN, their absent/no-match variants, and
exact equality. Expected native input rows/op are 16 for matched arms, 0 for unmatched arms, and 2 for exact equality. Every RPC verifies scalar COUNT DISTINCT (8, 0, or 1). Fixture
visibility uses paged exact IN COUNT checks. This copies the existing historical
harness without changing fixture/query execution; additional request digests,
fixture timestamp logging and opt-in memory observations are outside timing.
The base timestamp depends on wall clock (previous UTC hour); each log records it
and hashes each concrete request. The source manifest fixes the fixture oracle.

Timing: three fresh Docker processes per size, rounds 0/1/2 rotate arms, one warmup
per arm, 100 measured sequential RPCs per arm. Go benchmark calibration may also
execute a one-request trial; it is not a retained sample. GOMAXPROCS=2, CPUs 0/1,
explicit two-CPU quota, 4 GiB container memory with no additional swap, explicit 3 GiB native allowance.
The warmup, fixture setup and readiness checks are excluded; transport, binding,
plan-cache lookup/compilation as encountered, live index checks, pattern creation,
execution and integer-result verification are included. CPU and Go allocations
are process-wide within the timing window. Only current source is measured.

Memory: first execute two additional fresh processes, one per size, `-benchtime=1x` and
P3_MEMORY=1. For each arm after warmup/trial, force GC and record heap, reset tracked
peak, execute 20 verified requests, then force GC and record heap and cumulative
allocations. Each request must settle for 20ms with zero live query reservations,
results, compile reservations, uncached plan bytes, in-flight plans, owner pins,
and idle ScanResourceSnapshot. A five-second timeout fails the probe. Record
memory counters before/after and cache/scan/batch ownership counters. Go retained
heap includes caches and unrelated background activity; its signed delta is not
per-query live memory. Cumulative reserved/allocated bytes are not retained heap.
Whole-container cgroup-v2 memory.peak is captured after each binary exits and
includes setup, index, runtime, shell, and page cache; it is not per-query peak RSS.
Timing samples exclude isolated-probe mode. No cancellation/failure injection,
concurrency, admission boundary, distributed latency, long-lived leak or
large-distinct capacity claim is made by this bounded campaign.
