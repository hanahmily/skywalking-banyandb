# P3 two-worker focused replay protocol

Frozen before measurement: 10,000 generated names, eight `item-MaTcH-*`
names, two timestamps per name, whole-value name index, two normally routed
shards, replicas=0, on two real data-node servers and one liaison. Servers and RPC client
share one Go process; network transport is real. No query-budget increase:
256 MiB vectorized allowance and unmodified default native/protector policy.

Three arms: LIKE `%MaTcH%` distinct names (8), exact first name (1),
and LIKE with `SUM(value::FIELD+1)` (32 over the same 16 selected rows).
Every RPC checks an independently generated integer answer. All names are
visibility-qualified in pages before timing. Query traces separately retain
worker identities and raw-row contributions. Existing generated Sigma/final
sigma ILIKE witness covers Unicode; see ../p3/correctness.json.

First one correctness/memory process, one RPC/arm plus 20 isolated cleanup
probes/arm. Then three fresh processes with 30 sequential verified RPCs/arm.
Each process uses four CPU quota/affinity, 8 GiB memory, no extra swap.
Separate final profiling process, 100 RPCs/arm; it is excluded from latency
summaries. No concurrent builds or other campaigns. Record cgroups, host load,
source and binary hashes. Latency is descriptive, no regression or speedup
claim and no invented tolerance. Preserve raw per-RPC latency and process
p50/p95, CPU and allocations. No repeat-until-pass policy.

Counters cover all nodes and client in one process. Per-worker/coordinator
CPU and reservation metrics are null where no role-scoped counter exists;
profiles provide function-level attribution only. Memory probes assert zero
live reservations/results/cache inflight/compile reservations/uncached bytes,
owner pins, native scan ownership, with 20 ms quiet after each query. Cumulative
charges are not live memory. Any rejected RPC is retained separately, excluded
from successful latency, and stops this arm's growth. No production edits.
