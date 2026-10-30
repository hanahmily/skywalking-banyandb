# Frozen P6 complete-export capacity protocol

Head1af21ec71f2b98bae44371e84e279b58929cc200,Go1.26.10,querymetrics;
freeze all source/generated/harness/runner/protocol/binary/image digests before runs.
No production changes, historical evidence edits, baseline latency comparison,
or repeated timing campaign. This is a bounded source-shaped capacity assessment.

Schema p6bench.m: storage-only.entity_id is the entity, unindexed;
searchable.service_id has one keyword inverted index; value/summation/count are
integer fields. Two naturally routed shards, no replicas/sharding key.
Groupg has service s%06d and1+g%3 entities e%06d_%d. Each entity has two
unique timestamps(base,base+1s),value1/2,summation1/2,count1.
N output groups require2*sum(1+g%3) input rows. Repeated entity identities across the two timestamps
are distinct-count deduplication witnesses. Shared previous UTC hour is frozen.
Writes use bounded streams, exact acknowledgement counts and no repeated data
sweep. Three indexed group checks(first,middle,last) establish setup visibility.

Complete export, no LIMIT/OFFSET:
```sql
SELECT service_id, COUNT(DISTINCT entity_id) AS distinct_count
FROM p6bench.m
WHERE time >= ? AND time < ? AND value::FIELD > 0
GROUP BY service_id
ORDER BY distinct_count DESC, service_id ASC
```
Independent oracle enumerates count tiers3,2,1 then ascending group identities.
Check every row/count/order; any missing/extra/wrong result is a correctness
failure, never a capacity rejection. Retain full decoded outputs and oracle,
request/fixture/output hashes, protobuf response size and decoded output bytes.

First one1k distributed small qualification on two real data workers+liaison,
4CPU8GiB aggregate container,default256MiB allowance per node; trace verifies
both worker contributions and total raw input rows. No distributed capacity claim.
Then one staged standalone sweep1k→10k→100k OUTPUT groups, fresh process each.
Standalone2CPU4GiB,no extra swap,GOMAXPROCS2,default256MiB query tracker and
75% process protector(3GiB). Preserve default gRPC client4MiB receive limit;
server incoming-request16MiB limit is separate. Observe actual cgroup settings,
CPU throttling,peak memory,host load. Coordinate no concurrent team tests/builds.

Per stage: setup,quiet20ms ownership,GC,reset query peak,one complete export,
check full answer,quiet cleanup,GC,small indexed samecluster recovery.
Capture single-call wall/CPU/alloc/GC (not p50/p95),global tracked peak,
heap before/at response/afterGC,aggregate/native/order counters before/after.
Sample scan/aggregate/sort/native/query live reservations every1ms as LOWER
BOUNDS on owner peaks, not additive exact simultaneous residency. No isolated
response live counter or COUNT DISTINCT key-capacity counter exists: null.
Sort completion peak/capacity counters are cumulative perexecution snapshots.
Actual response protobuf bytes are observed, not an inferred live transport peak.
Monitor overhead belongs to this diagnostic, not production latency.

Only ResourceExhausted is an admissible capacity/response rejection. Retain
exact error; distinguish client receive-limit errors from query admission.
After each rejection assert all live owners zero+quiet20ms and samecluster
indexed recovery. Repeat rejection exactly once for reproducibility, then stop
larger sizes. Nonreproducible rejection stops inconclusively; never silently
rerun success,raise budgets,replace export with pages,or benchmark failed calls.
Required result residency is not automatically avoidable. Missing response total/
truncation metadata and stateless page consistency remain separate A4/A5 limits.
