# P5 exact decimal cost and ownership

All **7,000 timed RPCs** pass, as do eight default-policy qualification arms,
eight isolated single-request ownership probes, and four conditional profiles.
Ordinary integer SUM base/head performance remains inconclusive. Decimal SUM adds
measured feature cost while preserving exact fixed-scale output. Input projection
borrows existing numeric vectors; profiles identify setup and formatting costs
but establish no removable work. **No production fix is justified.**

## Identity and reproduction

Measured head `a948c94f2caafd26ff76fb6ec51eb16ff8c9f1c1`, ordinary-query base
`39d33bc5d8b49ea7ee33c253de2b476648637669`, Go1.26.10, querymetrics tags.
[Manifest](evidence/manifest.json) freezes complete source/API, identical harness,
runner/protocol, Docker image and binary hashes. The base is an isolated Git
archive with tracked Go/module inputs checked against Git; identical generated
protobuf files are supplied only after checking unchanged proto schemas.

The fixture contains 64/8192 positive integer rows, eight entities, value=i+1,
entity r(i/(rows/8)), offset i%(rows/8), and shared base time
`2026-10-30T01:00:00Z`. Independent math/big coefficient sums and a separate
fixed-scale renderer produce exact strings: scalar decimal totals
`0.0000002080` and `0.0033558528`. Per-entity COUNT checks visibility. Every
response is decoded and compared exactly; raw records retain fixture/request/
oracle hashes, requests and expected strings. The summarizer checks ordinary
base/head request hashes and expectations identical.

[Semantic review](source-review.md) and [provenance](qg3-correctness.json) cover
15 focused tests plus 29 actual It specs in each real RPC topology:156 successes
and three expected rejections each. The independent [large-value oracle](independent-oracle.json)
includes negatives, NULL and ten 18-digit coefficients whose exact group sum
`999999999.9999999990` exceeds int64 coefficient range. Focused tests independently
check precision38 overflow, cross-scale comparisons/IN, formatting and ownership.
These checks extend beyond the positive-value performance fixture.

```sh
python3 verification/qg3bench/p5/run.py build --out /absolute/fresh-output
python3 verification/qg3bench/p5/run.py prepare --out /absolute/fresh-output
# Exclusive measurement lane: no concurrent builds/tests/profiles.
python3 verification/qg3bench/p5/run.py timing --out /absolute/fresh-output
python3 verification/qg3bench/p5/run.py profiles --out /absolute/fresh-output
python3 verification/qg3bench/p5/summarize.py /absolute/fresh-output
python3 verification/qg3bench/p5/extract_profiles.py /absolute/fresh-output /absolute/fresh-output/current.test
```

[Protocol](protocol.md) was frozen before measurement: five alternating fresh
process pairs, 20 warmups, two cache checks and 100 verified timed RPCs per arm.
Observed containers use CPUs0/1, two-CPU quota, GOMAXPROCS2, 4GiB memory with no
extra swap. Default query tracker256MiB and process protector75% are unchanged.
No forced GC occurs inside timed arms; automatic GC remains enabled and its
process CPU is recorded. All 37 processes exit0; no measurement reruns or rejected admissions occurred.
The postprocessing field-name correction is retained in
[analysis-correction.json](evidence/analysis-correction.json), with raw evidence unchanged.
Scoped lint, independent oracle unit test and Python compilation passed.

## Fixed observations

Values below average five process quantiles; they are not pooled request
quantiles. CPU, allocation and objects are process-wide means per RPC, including
co-located client/server and background work.

| Rows | Revision | Arm | Mean process p50 ms [range] | Mean process p95 ms | CPU ms/RPC | Allocated B/RPC | Objects/RPC |
|---:|---|---|---:|---:|---:|---:|---:|
| 64 | base | group_int | 0.468 [0.444, 0.501] | 0.815 | 0.713 | 142082 | 1405 |
| 64 | base | scalar_int | 0.448 [0.383, 0.569] | 0.709 | 0.671 | 105027 | 1104 |
| 64 | current | group_decimal | 0.477 [0.463, 0.500] | 0.893 | 0.770 | 159480 | 1551 |
| 64 | current | group_int | 0.474 [0.443, 0.523] | 0.887 | 0.739 | 145321 | 1442 |
| 64 | current | scalar_decimal | 0.441 [0.400, 0.466] | 0.633 | 0.651 | 115324 | 1190 |
| 64 | current | scalar_int | 0.427 [0.399, 0.494] | 0.714 | 0.665 | 107385 | 1136 |
| 8192 | base | group_int | 2.314 [1.960, 2.548] | 3.839 | 2.956 | 550914 | 1479 |
| 8192 | base | scalar_int | 1.410 [1.116, 1.492] | 2.824 | 1.972 | 511877 | 1169 |
| 8192 | current | group_decimal | 2.266 [1.827, 2.656] | 4.101 | 2.933 | 567853 | 1584 |
| 8192 | current | group_int | 2.079 [1.750, 2.273] | 3.856 | 2.727 | 556039 | 1479 |
| 8192 | current | scalar_decimal | 1.437 [1.067, 1.567] | 3.114 | 2.017 | 522831 | 1223 |
| 8192 | current | scalar_int | 1.351 [1.020, 1.442] | 2.753 | 1.947 | 511876 | 1169 |

The primary 8192-row ordinary scalar comparison has paired mean p50 difference
−0.059ms, geometric ratio0.956, bootstrap90% interval[0.837,1.088]. All ordinary
controls' ratio intervals include1. Five-pair diagnostic uncertainty and no
predeclared practical tolerance preclude noninferiority/regression certification.
[Comparisons](evidence/comparisons.json) retain all paired differences and bounds
(20,000 draws,seed1412). No adaptive confirmation was added.

Decimal versus integer is a feature-cost comparison with different precision and
output contracts. At8192 rows scalar decimal adds0.086ms p50 (ratio1.062,
interval[1.046,1.081]) and10,956 allocated B/RPC; grouped adds0.188ms
(ratio1.087,[1.056,1.122]) and11,814 B/RPC. At64 rows scalar p50 differences have
mixed signs, while allocation increases in all five rounds by6.8–9.2KB/RPC
(mean7,939B). This allocation result triggers the predeclared small-scalar profiles.

Changed-binding cache/bypass controls alternate equal32-row half-windows, with
independent exact answers `0.0000000976` and `0.0000001104`. All timed cached arms
record100hits/no compiles; all bypass arms record100bypasses/compiles. Bypass adds
mean0.171ms p50 (ratio1.323,[1.095,1.630]),24,035B and0.220ms process CPU/RPC.
One paired latency difference is negative; allocation differences are all positive.
Cache hits still bind fresh parameters and compile execution-local expression
operators; these are not immutable-plan cache misses.

Both integer and decimal arms observe zero native aggregate executions here.
Decimal uses group-expression input and local exact aggregation, not decimal wire
frames. At8192 rows it evaluates8192 input keys across five batches per RPC, with
**zero full-column allocations and zero buffer bytes**;64 rows use one batch.
Ordinary integer has no group-expression execution. These counters support typed
input borrowing; they do not imply zero allocations throughout the query.

## Isolated memory and profiles

Each probe follows warmup,20ms quiet ownership,GC,tracked-peak reset,one verified
RPC,quiet cleanup andGC. All query reservations, scan owners, results, pins,
compile/uncached reservations and expression/native/aggregate live bytes return
to zero. Cache retained plan bytes remain intentionally owned.

| Rows | Arm | Tracked peak B | Aggregate state capacity charge B | Retained heap delta B |
|---:|---|---:|---:|---:|
|64|scalar_int|1855907|16|-528656|
|64|group_int|2044279|128|-442280|
|64|scalar_decimal|1876403|32|-398664|
|64|group_decimal|2074999|256|-83344|
|8192|scalar_int|34602718|16|-905784|
|8192|group_int|38917074|128|-977744|
|8192|scalar_decimal|34623214|32|45736|
|8192|group_decimal|38947986|256|-77024|

State is16B/group integer versus32B/group decimal (count plus padded Int128).
Capacity charges are cumulative allocation accounting, not per-owner live peaks.
The8192 grouped decimal charge256B divided by global tracked peak38,947,986B is
0.000657%, a diagnostic comparison, not literal simultaneous memory share.
It is below the declared10% owner-prioritization trigger, so no high-group probe
was added. Eight groups do not establish high-cardinality capacity. Retained heap
deltas are noisy post-GC observations, not actual peak heap or leak proof;
cumulative allocated/reserved bytes also are not live memory.

The [conditional decision](evidence/profile-decision.json) uses all five complete
rounds. Four1000-RPC profiles cover64-row scalar integer/decimal and32-row decimal
cached/bypass. Raw CPU/alloc profiles and flat/cumulative text are retained in the
three `evidence/profile*` directories. Allocation profiles subtract before from
after with512KiB sampling. In the64-row decimal profile, sampled allocation
attributes3MB flat to expressionOperator.compile,1MB flat to ResolveExpression,
2MB cumulative to MeasureTemplate.Bind and2MB cumulative to wrapGroupExpressions.
These demonstrate setup owners, not independently additive phase totals.
The Int128.String formatter has0.50MB sampled flat allocation; output string
formatting is required by the current contract. The cached/bypass setup profiles
likewise expose binding and expression compilation; bypass compiler counts verify
immutable-plan compilation even where sampled stacks do not resolve it.

CPU samples total590ms(integer),620ms(decimal),670ms(cached32-row),700ms(bypass).
Wide AddInt64 and decimal formatting have no resolved CPU samples in these short
captures, so their isolated CPU cost remains unknown, not zero. Source inspection
identifies checked two-word addition and stack-digit formatting; profiles cannot
prove either is a dominant removable cost. Profile allocation totals include
profiler setup (e.g.1.16MB StartCPUProfile in the64-row decimal profile); exclude
that known overhead from decimal-owner attribution. Profiles are separate from
unprofiled timing and their sampled totals are not exact allocation deltas.

Observed whole-container peaks span17,997,824–47,407,104B, including fixture,
indexes,runtime and page cache. No cgroup CPU throttling was observed. Host1-minute
load spans2.84–6.97 across preparation/timing/profile runs despite the exclusive
team lane; external scheduling and shared-background variation remain. This is
standalone evidence, not distributed performance or a memory-capacity certification.
Unmeasured phase CPU/allocations and per-owner live peaks are explicitly null in
[measurement-limits.json](evidence/measurement-limits.json). All portable evidence
has [artifact hashes](evidence/artifact-hashes.json); historical QG3 evidence is
preserved separately.
