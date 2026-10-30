# Frozen P5 decimal protocol

Head a948c94f2caafd26ff76fb6ec51eb16ff8c9f1c1 versus ordinary-query base
39d33bc5d8b49ea7ee33c253de2b476648637669; identical harness, generated API,
Go1.26.10, querymetrics. Base tracked Go/modules checked against Git archive.
No production changes or historical evidence edits.

Fixture: 64/8192 integer rows, eight entities, value=i+1, timestamp offset
 i%(rows/8), entity r(i/(rows/8)); same fixed previous UTC hour across revisions.
Independent math/big coefficient sums and fixed-scale10 rendering give decimal
oracle strings; no production decimal helper or floating-point oracle. Integer
SUM and SUM(DECIMAL_FROM_SCALED(value,10)), scalar/grouped, are four current
arms. Base supports only two integer controls. Integer and decimal have different
precision/output contracts, so their difference is feature cost, not regression.
Grouped COUNT verifies every entity is visible. Every result is compared exactly.

Two CPUs0/1, two-CPU quota, GOMAXPROCS2, 4GiB memory, no extra swap; default
256MiB QL tracker plus75%-cgroup process protector (3GiB here), no override.
Record source/API/binary/image/fixture/request/oracle hashes, Docker settings,
cgroup memory peaks, CPU throttling and host load. No concurrent team builds,
profiles or tests while timing. Co-located client/server/background costs remain
process-wide, not server-only.

Preparation: qualify all4 current arms at both sizes with2 verified calls each;
assert no native decimal reduction. Memory:20warmups+2cache checks, idle20ms,
GC, reset tracked peak, exactly1 verified RPC, idle20ms, GC/heap snapshots.
Require query/scan/cache/pins and aggregate/expression/native live owners zero.
Capture aggregate state bytes/capacity deltas separately from global peak.

Fixed timing: five alternating fresh-process base/current pairs per size,
rotating arms. Each arm has20warmups+2cache assertions+100timed verified RPCs.
Base2/current4 arms; total6000timed RPCs. Five additional alternating cached/
bypass pairs at64rows use scalar decimal SUM with first/second half-time-window
bindings, equal32-row work and different exact answers0.0000000976/0.0000001104;
20+2+100each, another1000timed RPCs. No forced GC/profiling in timing.

Primary diagnostic:8192ordinary integer scalar SUM p50 base→head. Other controls,
decimal/integer feature cost, and cache comparison support interpretation.
Nearest-rank per-process p50/p95 over100durations; mean CPU/allocated bytes/
objects/GC CPU perRPC. Paired-process bootstrap20000draws,seed1412,mean paired
absolute differences and geometric mean ratios,5th/95th percentile bounds.
No SLO/tolerance invented, no adaptive repetition or selective rerun. Five-pair
screening cannot certify noninferiority/regression.

Conditional profiles evaluated only after all five current64-row rounds finish:
if decimal-minus-integer scalar p50 OR allocation bytes is positive in every
round, profile1000RPCs each for64-row scalar integer/decimal, plus changed-binding
32-row scalar decimal cached/bypass in separate processes. CPU surrounds calls;
allocation before/after profiles are subtracted,512KiB sampling rate. Attribute
setup/input projection/wide addition/fixed-scale formatting separately through
stacks/counters, with unmeasured isolated timings/owner peaks explicitly null.

Conditional high-group diagnostic: evaluate8192group-decimal aggregate state
capacity divided by isolated global tracked peak. If at least10%, declare one
bounded high-group probe before executing it; otherwise do not expand. This is
an owner-prioritization criterion, not a capacity/SLO pass threshold. Eight groups
cannot establish high-cardinality decimal memory. A new representation/fix needs
measured avoidable work and exact signed precision38/string compatibility.
