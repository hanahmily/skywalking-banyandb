# Frozen P3 standalone follow-up

Current production source bdd1114d5a69906da7f51a2e1f94e385e02ade3d;
ordinary-query base 39d33bc5d8b49ea7ee33c253de2b476648637669. Identical harness,
generated API files, Go1.26.10 and querymetrics build tags. No production changes.
Fixture copies prior 1k/10k names, two timestamps/name, eight `%MaTcH%` matches,
independent fixture selection, IN visibility pages, keyword inverted name index.
All requests/counts checked, fixture and concrete request/oracle digests recorded.
A single fixed previous-UTC-hour timestamp is shared across the campaign.

All standalone processes use CPU0/1, two-CPU quota, GOMAXPROCS=2, 4GiB memory,
no added swap. **No native allowed-bytes override**: default-policy qualification
is separate from historical 3GiB replay. Query allowance defaults to 256MiB;
process protector is derived from cgroup memory. Record Docker observations,
cgroup memory peak/limits and CPU throttling, and host load before/after.

Pre-timing: qualify all seven existing arms at both sizes, retain failures with
quiet ownership and exact-query recovery. Do not time a rejected arm. Separate
memory mode warms 20 requests, requires idle ownership and resets tracked peak
before exactly ONE observed RPC/arm; 20ms quiet ownership and forced-GC retained
heap check afterward. Profiles at10k cover LIKE, ILIKE, exact, 100 requests each;
CPU profiling surrounds only requests, alloc profiles before/after permit
subtraction (sampling rate recorded). Profiling and GC never overlap timings.

Timing follows only after all builds/tests/profiles across agents finish:
- Default current cost curve: five fresh processes/size, rotating seven arms,
  20 warmups +100 verified timed RPCs per arm.
- Primary diagnostic comparison: 10k exact-name query, five fresh-process
  base/current pairs, alternating order, same20+100.
- Representative cache comparison: 1k exact query alternating two existing names
  with equal work, five fresh-process cached/bypass pairs, alternating order,
  same20+100. Separate absent→present(0→1) correctness witness and explicit
  hit/compile/bypass assertions detect stale bindings.

Per-process p50/p95 are nearest-rank empirical quantiles of100 RPC durations;
CPU, allocation bytes/objects and GC CPU are process-wide mean costs/RPC.
Five paired-process bootstrap resampling (20,000 draws, seed1412) reports
uncertainty of mean paired p50 differences and geometric mean ratios. No DPO
SLO or arbitrary tolerance is invented: this is diagnostic, with no automatic
non-inferiority/regression verdict or adaptive confirmation. Preserve every run.
Do not silently retry, drop arms, lower request counts, or change policy.

Raw rows come from native counters. Dictionary visits, expanded terms, per-owner
live automaton bytes are null where no RPC counter exists; allocation/CPU
profiles supply sampled attribution, not live residency. Client/server/background
are co-located and not separable. This is contains/leading-wildcard selection,
not a general prefix/selectivity sweep. Precomputed IN excludes discovery.
