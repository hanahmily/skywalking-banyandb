# P3 current-source LIKE/ILIKE verification

This report covers the initial current-source campaign, not the entire P3
investigation. The [standalone follow-up](../p3followup/results.md) adds
default-policy qualification, the unchanged-query base/head comparison,
changed-binding cache checks and profiles. The [two-worker replay](../p3distributed/results.md)
covers partitioned worker contributions and composed aggregation. The original
observations below remain unchanged.

The current-source campaign passed: 42 retained process/arm timing samples
(4,200 result-verified timed RPCs), 14 isolated memory observations (280 additional
result-verified RPCs), and all expected native row counts. All six predetermined
timing processes and two memory processes succeeded; no timed run was retried.
Memory processes ran first. Separate generated integration verification passed
34/34 specs and 246 verification RPCs for **each** standalone and distributed
execution; see [correctness evidence](correctness.json).

Current HEAD is `5e78df39d0ca171aa61b97dbb4383dab32c434e6` with the uncommitted P2 generated suite
and P3 harness captured by the [source/binary manifest](evidence/manifest.json).
Go: `go version go1.26.10 linux/amd64`. The [fixed protocol](protocol.md) and [runner](run.py)
are hashed in that manifest. The image, observed Docker limits, commands, log
hashes and host load are retained in `evidence/*-run.json`; complete compact raw
logs include concrete request hashes and dynamic fixture timestamps. Binary SHA256:
`12a938922ed51d5514a6c0876ad7e4ff02c1787e93fada374df73e9992f2d5e8`. The local binary and patch stay under ignored
`local/final/`; portable observations and source hashes are under `evidence/`.

## Timing and query work

The table gives the median and range of **three process-level mean latencies**,
not request-level median/p95. CPU/allocations are process-wide during timing.
All raw per-round values are in [samples.json](evidence/samples.json).

| Names | Arm | Process mean latency ms: median [min, max] | CPU ms median | B/op median | allocs/op median | Tracked peak B | Native rows/op |
|---:|---|---:|---:|---:|---:|---:|---:|
| 1000 | ILIKE | 4.428 [4.351, 4.437] | 5.468 | 1076967 | 19844 | 2785056 | 16 |
| 1000 | ILIKE_no_match | 4.508 [4.097, 4.660] | 5.252 | 992655 | 19371 | 1093964 | 0 |
| 1000 | IN | 0.569 [0.555, 0.571] | 0.737 | 126953 | 1220 | 1705272 | 16 |
| 1000 | IN_no_match | 0.293 [0.284, 0.306] | 0.421 | 35745 | 563 | 14880 | 0 |
| 1000 | LIKE | 4.190 [4.061, 4.289] | 5.283 | 1075570 | 19847 | 2785056 | 16 |
| 1000 | LIKE_no_match | 4.208 [4.152, 4.260] | 5.232 | 993218 | 19373 | 1093964 | 0 |
| 1000 | exact_control | 0.359 [0.356, 0.361] | 0.546 | 98380 | 711 | 1232542 | 2 |
| 10000 | ILIKE | 33.770 [33.247, 33.808] | 36.618 | 8092258 | 167262 | 11999277 | 16 |
| 10000 | ILIKE_no_match | 33.602 [33.057, 33.894] | 36.255 | 8007358 | 167053 | 10308185 | 0 |
| 10000 | IN | 0.441 [0.420, 0.448] | 0.683 | 140664 | 1397 | 1713988 | 16 |
| 10000 | IN_no_match | 0.229 [0.220, 0.235] | 0.445 | 54414 | 704 | 14880 | 0 |
| 10000 | LIKE | 33.997 [33.636, 34.829] | 36.565 | 8093128 | 167530 | 11999277 | 16 |
| 10000 | LIKE_no_match | 32.769 [32.214, 33.675] | 35.291 | 8006725 | 166730 | 10308185 | 0 |
| 10000 | exact_control | 0.301 [0.292, 0.309] | 0.552 | 117130 | 857 | 1232554 | 2 |

Matched pattern RPCs rise from about 4.2–4.4 ms at 1,000 names to about 34 ms at
10,000 names; equivalent precomputed IN stays under 0.6 ms in this fixture.
No-match patterns cost about as much as matched patterns despite returning zero
native input rows. This is consistent with dictionary-search work before row
execution; it does not measure dictionary visits directly. All matched pattern
and IN arms consume exactly 16 native input rows, unmatched arms zero, exact two.
IN name discovery is outside timing, so this is not an end-to-end discovery
comparison. These are contains patterns with leading and trailing wildcards,
not a prefix/selectivity sweep. There is no measured baseline regression or
improvement claim, and the cost difference alone does not justify a production
change.

## Memory and ownership

Each isolated probe starts after warmup and GC, resets tracked peak, performs 20
sequential verified RPCs and requires 20ms quiet ownership after **every** RPC,
then forces GC again. Live reservations, query results, compile reservations,
uncached plan bytes, in-flight cache work, owner pins, cursor refs, scan results,
scan reservations and pending decode tasks are all zero. The two standing decode
workers are idle infrastructure, not leaked request ownership. Cache entries can
remain intentionally retained. [Memory observations](evidence/memory.json) retain
all raw counters, heap snapshots and allocation deltas.

| Names | Arm | Tracked peak B | Cumulative lease charge over 20 RPCs B | Post-GC heap delta B |
|---:|---|---:|---:|---:|
| 1000 | LIKE | 2785056 | 55767620 | 81304 |
| 1000 | ILIKE | 2785056 | 55767620 | -5312 |
| 1000 | IN | 1705272 | 34159460 | 92184 |
| 1000 | LIKE_no_match | 1093964 | 22232500 | -53608 |
| 1000 | ILIKE_no_match | 1093964 | 22232500 | -19328 |
| 1000 | IN_no_match | 14880 | 374980 | -76616 |
| 1000 | exact_control | 1232542 | 24709700 | 94448 |
| 10000 | LIKE | 11999277 | 240052040 | -20472 |
| 10000 | ILIKE | 11999277 | 240052040 | 83520 |
| 10000 | IN | 1713988 | 34333780 | 84080 |
| 10000 | LIKE_no_match | 10308185 | 206516920 | -16528 |
| 10000 | ILIKE_no_match | 10308185 | 206516920 | -38328 |
| 10000 | IN_no_match | 14880 | 374980 | 11064 |
| 10000 | exact_control | 1232554 | 24709940 | 118000 |

Tracked peak reservation grows with dictionary size (matched LIKE/ILIKE about
2.79 MB to 12.00 MB; no-match about 1.09 MB to 10.31 MB). This is accounting peak,
not actual Go heap or RSS. Lease charge is cumulative reservation activity and
must not be read as retained memory. Signed post-GC heap deltas range from
−76,616 to +118,000 bytes and include caches, pools and background activity;
these small bounded observations do not establish long-lived leak freedom.

Whole-container cgroup-v2 peaks, including setup/index/runtime/shell/page cache,
range from 24.9–25.6 MiB across 1k timing processes and 63.9–73.7 MiB across 10k
timing processes. The isolated memory processes peak at 23.9 and 66.3 MiB.
These are **not per-query peak RSS**; see [raw process memory](evidence/process-memory.json).

## Verification and limits

The copied harness is deliberately isolated from the historical harness; its
fixture and execution logic must be kept aligned if reused. The copied harness builds with `querymetrics`; repository golangci formatting and
lint pass using golangci-lint 2.14.0 (Go 1.26.9), Python compilation passes, and
`git diff --check` passes. The default PATH linter was incompatible (built with
Go 1.25); no measurements came from that failed lint attempt. The runner verifies
source/binary identity before and after the campaign, log checksums, process
success, all 42 samples, exact native rows, and all 14 memory observations.

Two pinned CPUs, explicit two-CPU quota, GOMAXPROCS=2, 4 GiB memory/no additional
swap and 3 GiB native allowance were observed as configured. Host one-minute load
was 5.17–6.05; no concurrent validation was run by this session during timing,
but this was a shared host. Three rounds are descriptive evidence, not statistical
significance. Results apply to these two small dictionaries, warm sequential
COUNT DISTINCT queries and standalone RPC transport. Distributed correctness is
verified separately; distributed latency, concurrent queries, cold starts,
admission boundaries and large-distinct scaling are outside this campaign.
Historical reports in the parent directory remain unchanged and are not combined
with these measurements.
