# P3 focused two-worker pattern replay

Qualification passed on two real data-node servers and one liaison using real
BydbQL gRPC. Servers and client share one Go process. The partitioned fixture
has 10,000 names, two timestamps each, two shards, zero extra replicas and a
whole-value inverted name index. Normal routing gives the eight matched names
10 raw rows on one worker and six on the other. Traces assert both distinct
worker identities and total 16 rows for LIKE and computed SUM; exact-name
control asserts two raw rows, from one worker. The coordinator answers are
independently known: 8 distinct names, 1 exact name, 32 for SUM(value+1).

Every page of fixture names is visibility-qualified via QL before measurement.
The narrow Unicode Sigma/final-sigma ILIKE witness is the unchanged generated
case retained by [prior correctness evidence](../p3/correctness.json); production
sources have not changed since that verification. This campaign does not claim
an independent Unicode replay.

The default-policy qualification uses explicit 256 MiB vectorized query budgets,
no native `allowed-bytes` override, and the default protector percentage 75%.
The four-CPU/8-GiB cgroup implies a 6-GiB protector ceiling shared by these
same-process servers; this is separate from per-query admission. It is not
three separately isolated production nodes. Observed limits and exact commands
are retained in `evidence/*-run.json`.

All 60 isolated memory probes pass cleanup after every RPC. Peak process-wide
reservations are 65,729,504 B LIKE, 51,832,363 B exact and 65,728,210 B computed
SUM. Post-GC heap deltas are +285,960, +205,872 and +320,256 B, respectively.
These include servers, client, caches, pools and background work. All live query
reservations, results, owner pins, cache inflight/compile/uncached ownership and
native scan ownership return to zero for a 20-ms quiet interval. Eight idle
decode workers are infrastructure. These bounded probes do not prove indefinite
leak freedom.

The first setup used replicas=1 and each worker returned all16 matched rows;
the coordinator deduplicated them correctly. That exploratory setup is retained
as `initial-replicated-topology.log`, excluded from the final partitioned
campaign. Replicas were then corrected to0 and worker contribution assertions
added. The final qualification binary is identical to the measurement binary;
the runner subsequently gained license text, generated-Go source hashing and
resource assertions. Its earlier identity remains in `qualification-manifest.json`.

Per-role CPU and reservations are null: the available counters are process-wide.
Function-level CPU profiles can identify worker and coordinator stacks but cannot
recover isolated per-node CPU or live reservation peaks. This is a descriptive
feature-cost observation, not a base/head regression comparison or proof that
index discovery is unnecessary. No production code changed.

## Fixed descriptive measurements

All three fresh timed processes passed: **270 verified timed RPCs**, nine
process/arm observations. Go's one-RPC calibration invocations are retained in
`samples.json` and explicitly excluded by the summarizer's 30-sample filter.
A separate profile process passed 300 verified measured RPCs plus calibrations.
No rejected RPC, retries, concurrent builds or other benchmark campaigns occurred.
Host one-minute load during timing was 0.62–0.77; affinity does not imply
exclusive CPU ownership. All run files retain CPU throttling and cgroup peak.

| Arm | Process p50 ms (three rounds) | Process p95 ms (three rounds) | Median CPU ms/RPC | Median B/RPC | Median allocs/RPC |
|---|---|---|---:|---:|---:|
| LIKE distinct | 14.468, 14.218, 14.110 | 18.611, 16.951, 17.703 | 32.422 | 9,287,642 | 172,377 |
| Exact distinct | 0.635, 0.661, 0.617 | 1.113, 1.112, 0.760 | 1.651 | 183,133 | 1,943 |
| LIKE computed SUM | 14.381, 14.347, 14.996 | 20.282, 18.035, 20.088 | 33.485 | 9,236,066 | 172,402 |

LIKE and computed SUM both read exactly16 native rows per RPC, exact2.
The difference between these shapes is not an equivalent-work comparison:
COUNT DISTINCT transports names, while SUM(value+1) transports numeric values.
Trace frames total976 B for distinct,752 B for computed SUM; worker matches
are3+5 series. These traces separately measure instrumentation-bearing spans;
their wall times are not CPU or additive components of untraced latency.
Dictionary visited terms, expanded-term count, automaton-only allocation,
and isolated transport CPU are unavailable (null), not zero.

The 100-RPC LIKE CPU profile has3.71 seconds of samples: series-index search
accounts for71.43% cumulative, MatchTermsSet67.65%, dictionaryMatchCandidates
50.67%, and background GC11.59%. These are overlapping stack totals and must
not be added. This identifies dictionary search as a large cost owner, without
showing it does unnecessary work. No material integration regression is
established by three descriptive processes; no production optimization follows.

Verdict: **correctness and default-budget admission qualified for this bounded
partitioned fixture; feature cost observed; no demonstrated defect**. Per-role
resource attribution remains unavailable. Different CPU count, topology,
replication and budget policy prevent comparing these medians to the previous
standalone3-GiB historical replay as a claimed speedup.

## Reproduction and identities

Build before any measurement; do not run alongside other campaigns:

```sh
go test -c -tags=querymetrics -o verification/dpo85bench/p3distributed/local/bench.test ./verification/dpo85bench/p3distributed
python3 verification/dpo85bench/p3distributed/run.py qualification
python3 verification/dpo85bench/p3distributed/run.py round-0
python3 verification/dpo85bench/p3distributed/run.py round-1
python3 verification/dpo85bench/p3distributed/run.py round-2
python3 verification/dpo85bench/p3distributed/run.py profile
python3 verification/dpo85bench/p3distributed/summarize.py
```

Use a fresh evidence location: the runner refuses overwrites and changed frozen
identities. Raw profiles retain both calibration `*-1.cpu.pprof` and final
`*-100.cpu.pprof`; portable top/cumulative reports use only the latter.
Scoped golangci formatting/lint passed, Python compilation passed and all raw
log hashes, nine expected samples, row counts and process statuses validate.

Source HEAD: `bdd1114d5a69906da7f51a2e1f94e385e02ade3d`; Go: `go version go1.26.10 linux/amd64`.
Binary SHA256: `d4bd00d814c7059758e1690c6b74084de9fbef26a67c80a0a7b97bfb4499e825`.
Image ID: `sha256:09c665d7ce2302ef883da73a11f72161ee18a66336dfb7d03f52fd817875d877`.
