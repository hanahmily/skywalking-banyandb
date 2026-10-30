# Frozen P4 composition protocol

Head c59b950f5a2bbbfa015475f2dfadae471e53275e and ordinary-query base
39d33bc5d8b49ea7ee33c253de2b476648637669 use identical harness/API generation,
Go1.26.10 and querymetrics. Baseline archive tracked Go/modules are verified
before build. No production changes. Prior QG2 evidence remains historical.

Fixture:64/8192 rows,eight entities,integer value=i+1,prior=i,float fv=i+1;
time offsets i%(rows/8),entity r(i/(rows/8)). Independent loop computes scalar
and per-group sums; grouped COUNT verifies all entities visible before work.
Full windows span one hour from fixed previous UTC hour shared by both revisions.
Ordinary arms:single-entity float SUM,scalar integer SUM,grouped integer SUM.
Current adds float/scalar/group COALESCE and scalar/group SUM(prior+1),eight arms.
Float SUM/coalesce must increment native aggregate reduction counters on actual
RPCs. Group-expression and projection counters distinguish per-input expression
work from postaggregate output work; capacity charges are cumulative,not live.

Default policy:no allowed-bytes override;256MiB QL tracker,75%-of-cgroup process
protector (3GiB in these4GiB containers). CPU0/1,two-CPU quota,GOMAXPROCS2,4GiB,
no extra swap. Observe Docker resource/image settings,cgroup peak and CPU throttle
counters,host load. No other validation/profiles/builds during timed campaign.

Preparation:qualify all8 arms at both sizes,including2 verified calls/native
proof each. Unexpected rejection/error fails that cohort without timing it.
Memory:20warmups+2cache checks,idle20ms,GC,reset peak,exactly1 verified request,
idle20ms then GC/heap/counters. Require query/scan/cache/pin and expression/native
live ownership zero; no per-owner peak claimed. Profiling is separate:
8192scalar direct/coalesce/computed and grouped computed,plus64scalar computed
cached/bypass with changed half-window bindings;1000requests/profile,CPU only
around calls,allocation before/after subtraction,512KiB default sampling rate.
Profiles include sampling/serialization/background noise; exact setup vs eval
ownership attributed through call stacks/counters,missing durations remain null.

Timing:five alternating fresh-process base/current pairs per size. Each process
rotates its arms by round,20warmups+2cache checks+100timed verified RPCs/arm.
Base has3 arms,current8. Cache comparison:five alternating fresh-process
cached/bypass pairs at64 rows for scalar computed SUM,20warmups+2cache checks+
100timed calls. Alternate equal-sized first/second half-time windows (32 rows
per request) with independently different answers. Assert two cache hits/no
compiles or two bypass compiles before timing,and verify timed cache deltas.
Result checking/formatting/sorting identical between compared arms.

Primary diagnostic:8192 ordinary scalar SUM p50 base→head. Other direct controls,
composed/direct feature costs,and cache comparison are supporting. Per-process
p50/p95 use nearest-rank100samples;process CPU,allocation bytes/objects,GC CPU are
means/RPC,client+server+background included. Paired process bootstrap20000draws,
seed1412,reports mean paired absolute effects and geometric mean ratios with
5th/95th percentile bounds. No DPO tolerance or SLO invented;five-pair diagnostic
cannot certify regression/noninferiority. No adaptive extension or selective
reruns. No many-group/repeated-expression expansion absent measured implication.
A feature-cost difference alone does not justify a production optimization.
