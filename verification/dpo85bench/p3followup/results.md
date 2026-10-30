# P3 standalone follow-up: default policy, ordinary control and attribution

Default-policy qualification passes all seven LIKE/ILIKE/IN/exact arms at 1,000
and 10,000 indexed names. All **9,000 timed RPCs** pass across 30 fresh timing
processes; 14 isolated single-query memory observations return to idle ownership.
The unchanged exact-name query's five-pair base/current comparison is
**inconclusive as a regression/non-inferiority decision**: its p50 change is
−0.00228 ms, with diagnostic bounds spanning zero. The main pattern profile owner
is `storage.seriesUniverse` and native exact-candidate/projection work, not
wildcard compilation. **No avoidable work or production fix is proven.**

This completes the standalone follow-up to [historical P3](../p3/results.md).
[Separate distributed/combined/Unicode work](../p3distributed/results.md) covers
that execution boundary. It must not be inferred from these standalone numbers.

## Frozen inputs and reproduction

Current `bdd1114d5a69906da7f51a2e1f94e385e02ade3d` and base
`39d33bc5d8b49ea7ee33c253de2b476648637669` use the identical [harness](pattern_test.go),
Go `go version go1.26.10 linux/amd64`, querymetrics tags and immutable image
`sha256:09c665d7ce2302ef883da73a11f72161ee18a66336dfb7d03f52fd817875d877`. The base archive is isolated under `/tmp`; every tracked
Go/module file is checked against the exact Git archive before building. Only
ignored generated `api/proto` Go files are supplied identically; the runner
asserts that proto schemas do not differ. No current production Go file is copied
over the base. [Manifest](evidence/manifest.json) records source/API/binary hashes.

The names fixture is a separate whole-value indexed measure: two timestamps per
name, eight matches, matching distinct count 8, no-match 0, exact 1. Both revisions
share timestamp `2026-10-30T00:00:00Z`. Every successful call checks result
shape and integer value against the independent oracle. Raw observations retain
fixture names hash, full requests, protobuf-JSON request hashes and scalar oracle
hashes. Summaries add canonical JSON request hashes and verify paired requests
are identical; successful scalar result digests equal the oracle by assertion,
not a full serialized-response digest. Schema/index/generator identity is fixed
by the harness hash. Rebinding alternates two existing exact names with equal
work; a separate absent→present 0→1 witness checks stale bindings.

```sh
python3 verification/dpo85bench/p3followup/run.py build --out /absolute/new-output
python3 verification/dpo85bench/p3followup/run.py prepare --out /absolute/new-output
# Wait until other builds/tests/profiling are finished before timing.
python3 verification/dpo85bench/p3followup/run.py timing --out /absolute/new-output
python3 verification/dpo85bench/p3followup/summarize.py /absolute/new-output
```

The [protocol](protocol.md) fixes five processes/size for the cost curve,
five alternating exact base/current process pairs, five alternating cache/bypass
pairs, 20 warmups and 100 per-request observations/arm. It refuses repeated timing
runs and checks source/binary identity before/after. The summarizer checks all
35 successful processes, observation/log hashes, native rows, memory cleanup,
cache deltas, paired request identity and all quantiles from raw observations.
Portable [raw evidence](evidence/runs.json) includes all logs,100-sample arrays,
profiles, cgroup/host measurements and commands; binaries remain ignored locally.

One orchestration failure occurred before timing: the initial 1k qualification
passed, but its root-owned 0600 JSONL could not be read by the runner. A container
chmod recovered the original file; that successful qualification was **not
repeated**. [Amendment](evidence/amendment.json), both prior runner versions and
manifests preserve old identities. The final wrapper changes only output
permissions and validated prerequisite resume. Production source, binaries and
fixture time stayed identical; no successful measurement was discarded.

## Admission, latency and allocation

Docker-observed limits are CPU 0/1, two-CPU quota, GOMAXPROCS=2, 4 GiB memory and no
additional swap. No `--allowed-bytes` is passed. The code's default process
protector is 75% of cgroup memory, hence **3GiB here**, numerically equal to the
historical explicit override. The QL query tracker defaults to 256 MiB separately;
the process protector and per-query allowance have different roles. Numeric
protector/tracker defaults are source-derived, while container limits are
observed. This qualifies the actual default-policy path; it does not imply a
stricter numeric process limit than the earlier replay.

No admission rejection occurred. Qualification only accepts explicit
ResourceExhausted as a capacity result, repeats it once and checks exact-query
recovery; wrong answers/other errors fail. That rejection branch was not exercised
and is not claimed as new failure-cleanup evidence. Timing requires every arm to
be admitted plus completed memory/profile prerequisites.

Per-process p50/p95 are empirical nearest-rank quantiles of 100 RPCs; table entries
average five process quantiles. CPU/allocations/GC include client, server and
background work in the same process. Forced GC and profiling are outside timings.

| Names | Arm | Mean process p50 ms [range] | Mean process p95 ms | CPU ms/RPC | Allocated B/RPC | Objects/RPC |
|---:|---|---:|---:|---:|---:|---:|
| 1000 | ILIKE | 3.637 [3.584, 3.705] | 5.242 | 4.986 | 1081630 | 19935 |
| 1000 | ILIKE_no_match | 3.516 [3.481, 3.557] | 5.076 | 4.760 | 997627 | 19421 |
| 1000 | IN | 0.438 [0.405, 0.466] | 0.677 | 0.676 | 128623 | 1268 |
| 1000 | IN_no_match | 0.269 [0.265, 0.274] | 0.376 | 0.408 | 35582 | 564 |
| 1000 | LIKE | 3.631 [3.574, 3.673] | 5.277 | 5.009 | 1081889 | 19936 |
| 1000 | LIKE_no_match | 3.518 [3.485, 3.547] | 5.081 | 4.763 | 997109 | 19421 |
| 1000 | exact_control | 0.312 [0.306, 0.321] | 0.440 | 0.526 | 98129 | 717 |
| 10000 | ILIKE | 31.757 [31.645, 31.884] | 34.005 | 34.835 | 8086958 | 167220 |
| 10000 | ILIKE_no_match | 31.519 [31.149, 31.709] | 33.815 | 34.431 | 8001644 | 166740 |
| 10000 | IN | 0.431 [0.410, 0.506] | 0.522 | 0.722 | 149315 | 1429 |
| 10000 | IN_no_match | 0.216 [0.201, 0.266] | 0.335 | 0.446 | 52719 | 709 |
| 10000 | LIKE | 31.702 [31.386, 31.935] | 34.027 | 34.739 | 8087622 | 167222 |
| 10000 | LIKE_no_match | 31.857 [31.323, 32.770] | 34.044 | 35.093 | 7999425 | 166445 |
| 10000 | exact_control | 0.291 [0.277, 0.332] | 0.504 | 0.585 | 114030 | 853 |


All matched pattern/IN calls consume 16 raw rows, unmatched 0, exact 2. Contains
patterns with leading/trailing wildcards grow with dictionary size while selected
rows remain fixed. IN's list discovery is excluded, so its lower cost does not
establish a defect. These are not prefix/selectivity or concurrency sweeps.

## Same-query ordinary control and compiled-plan reuse

| Five-process mean | Base exact 10k | Current exact 10k | Cached exact 1k | Bypass exact 1k |
|---|---:|---:|---:|---:|
| p50 ms | 0.313316 | 0.311036 | 0.310372 | 0.354194 |
| p95 ms | 0.447244 | 0.429704 | 0.484458 | 0.561892 |
| CPU ms/RPC | 0.573114 | 0.548702 | 0.539400 | 0.585208 |
| Allocated B/RPC | 108793 | 109372 | 98098 | 120049 |
| Objects/RPC | 841.1 | 846.3 | 714.8 | 805.6 |

The primary base→current p50 mean paired difference is −0.00228ms and geometric
mean ratio 0.99285. Paired-process bootstrap (20,000 draws, seed 1412) gives
5th/95th-percentile bounds **[−0.01559,+0.01068]ms** and **[0.95063,1.03525]**.
Current exact allocation changes by +579 B/RPC (about +0.53%) and CPU by
−0.0244 ms/RPC across paired processes; these are supporting descriptive effects.
These are a two-sided 90% diagnostic interval/equivalent one-sided 95% bounds,
not a two-sided 95% interval. No practical SLO/tolerance was invented; this does
not certify non-inferiority or prove a material regression. There was no adaptive
20-pair confirmation because no important repeatable excess was established.

Cache→bypass adds 0.043822ms p50 (bounds[0.038168,0.048972]) and ratio1.14129
(bounds[1.12161,1.15950]); all five differences are positive. Bypass adds about
21,951 B/RPC and 90.8 objects/RPC. Ten separate absent/present witnesses passed;
timed cached arms each add 100 hits/no compiles, bypass arms 100 bypasses/compiles.
This supports reuse for this small exact query only. Bind/setup-only allocation
is not separately instrumented; total RPC allocations include compilation work.
[All paired effects](evidence/comparisons.json) and raw counter deltas are retained.

## Single-query memory and ownership

Each observation warms 20 requests, waits for idle ownership, forces GC, asserts
peak reset succeeds, runs **one** verified request and waits 20ms quiet before the
post-GC heap snapshot. All scan cursor/owner/result/reservation/task counters,
query reservations/results, plan in-flight/compile/uncached bytes and owner pins
return to zero. Two standing decode workers are idle infrastructure. Cache
entries/retained bytes are recorded and can remain intentionally live.

| Names | Arm | One-query tracked peak B | Post-GC heap delta B |
|---:|---|---:|---:|
| 1000 | LIKE | 2785056 | -100872 |
| 1000 | ILIKE | 2785056 | -100008 |
| 1000 | IN | 1705272 | -197024 |
| 1000 | LIKE_no_match | 1093964 | -11512 |
| 1000 | ILIKE_no_match | 1093964 | -9544 |
| 1000 | IN_no_match | 14880 | -47152 |
| 1000 | exact_control | 1232542 | -161704 |
| 10000 | LIKE | 11999295 | -62936 |
| 10000 | ILIKE | 11999295 | -97144 |
| 10000 | IN | 1713988 | -122656 |
| 10000 | LIKE_no_match | 10308203 | -7600 |
| 10000 | ILIKE_no_match | 10308203 | -12888 |
| 10000 | IN_no_match | 14880 | -5872 |
| 10000 | exact_control | 1232554 | -132816 |

Tracked peak is reservation accounting, not heap/RSS; cumulative LeaseChargedBytes
is neither live peak nor retained memory. Negative post-GC deltas include caches,
pools, background activity and GC timing; they do not show negative query memory
or prove long-term leak freedom. Memory-pass allocation deltas include quiet
ownership instrumentation and GC; use timing-mode B/RPC for ordinary churn.
Whole-container cgroup peaks across all 35 processes range 22.7–72.2MiB and include
setup/index/runtime/profiler/page cache, not per-query RSS. No cgroup CPU throttling
was recorded; host one-minute load ranged 0.80–1.93. Affinity is not exclusive CPU
ownership. [Missing counters](evidence/missing-counters.json) are explicitly null.

## CPU/allocation attribution

Separate 10k profiles run 100 verified RPCs each for LIKE, ILIKE and exact. CPU
profiling spans those calls only; allocation snapshots before/after are subtracted
with `go tool pprof -sample_index=alloc_space -base BEFORE AFTER`. Sampling rate
is 512 KiB. Full `.pprof` plus flat/cumulative/all-node text reports are retained in
[evidence/profile-10000-0-current-cache](evidence/profile-10000-0-current-cache/LIKE-alloc-all.txt).

| Cumulative profile owner | LIKE | ILIKE |
|---|---:|---:|
| seriesUniverse allocated share | 86.25% | 86.26% |
| seriesUniverse CPU share | 85.80% | 85.92% |
| projectCandidates allocated share | 45.80% | 47.35% |
| dictionaryMatchCandidates CPU share | 67.33% | 65.35% |
| wildcardCandidates allocated share | 1.02% | 1.10% |
| wildcardCandidates CPU share | 3.69% | 1.69% |

Cumulative rows overlap and must not be added. The dominant path is native series
universe construction through MatchTermsSet/exactCandidates and projection;
automaton intern allocation sampled about 1 MiB/LIKE 100 calls and 0.5 MiB/ILIKE 100,
well below the main owner. This corrects an overly broad attribution to wildcard
compilation. Unicode folding has no independently resolved sampled cost here;
no claim of zero folding overhead or ILIKE speed advantage follows.

Allocation subtraction still includes profiler serialization/start/stop and
background maintenance. `runtime/pprof` stacks are visible; the percentages above
are raw totals, not profiler-overhead-adjusted costs. Exact's short profile is
particularly dominated by profiler/background allocations, so it is not used to
infer exact-query allocation cost; use the unprofiled per-RPC counters. Profiles
identify an owner for investigation, **not proof that universe work is avoidable**.
A causal, same-query correctness-preserving comparison would be required before
optimizing it. No production change was made.

## Validation and verdict

Both revision binaries build. Repository formatting/lint with compatible
golangci-lint 2.14.0 pass; Python compilation and `git diff --check` pass. Frozen
source and binary hashes match after all runs. Historical evidence remains intact.
Verdict: **bounded cost/capacity established; ordinary-control regression decision
inconclusive; no confirmed defect or justified optimization**. Larger dictionaries,
production-scale admission, long-lived retention, generalized cache benefit and
separate server-only/worker-only owners remain outside these standalone results.
