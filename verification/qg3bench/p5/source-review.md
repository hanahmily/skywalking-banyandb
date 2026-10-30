# P5 exact decimal source and semantic review

P5 follows the authoritative performance verification plan and work packages in
`/mnt/d/banyandb-ql-design/docs/`:64/8192 rows, scalar/grouped integer and decimal
queries, changed bindings and isolated memory. Integer SUM is a feature-cost
control with a different output/precision contract. Add high-group capacity only
if state cost is implicated; eight groups cannot establish high-cardinality
capacity. No production change follows without an identified removable cost.

## Existing ownership and work

| Stage | Current implementation | Measurement interpretation |
|---|---|---|
| Setup/binding | `pkg/bydbql/plan_measure_aggregate_input.go:37` validates literal scale0–18, unwraps DECIMAL_FROM_SCALED to its int64 input recipe, deduplicates identical calls, and marks aggregate decimal/scale. Bind resolves input expressions at line105. | Plan cache hits still bind fresh parameters and build execution-local operators. Decimal is not a float conversion. |
| Input projection | `pkg/query/ql/group_expression.go:27` routes decimal source recipes through group-expression kernels. `expression_operator.go:432` borrows typed numeric columns; line476 reuses sufficient buffer capacity across batches. | Inspect GroupExpressionExecutionStats, including source decoder work; don't assume every decimal row creates an Int128 expression vector. Existing reuse is within an execution. |
| Wide sum | `pkg/query/ql/aggregate_state.go:140` calls Int128.AddInt64 on each non-NULL int64 coefficient. `pkg/decimal/decimal.go:47` uses signed-magnitude two-word add/subtract with checked precision38. | Expected exact wide arithmetic; not arbitrary-precision heap integers per row. Shared aggregate state is already compiled by source/type. |
| Group state | `aggregate_state.go:72` accounts8-byte count plus24-byte padded Int128=32 B/group, versus count+int64=16 B/group. Growth copies capacity lanes. | StateBytes is logical occupied state; StateCapacity includes allocated spare capacity. Neither equals whole-query native/input/output residency. |
| Output | Aggregate output is a typed decimal vector; `pkg/query/vectorized/measure/serialize.go:124` constructs the existing protobuf string field through Int128.String(scale). | Fixed-scale formatting is required by current output contract. `decimal.go:90` uses a39-byte local digit buffer and repeated word division, then string padding/sign/decimal-point construction. Attribute formatting allocations with profiles; do not replace with float. |
| Ownership | `pkg/query/ql/aggregate.go:328` clears group/state/readers/output and releases reservations once on Close. Expression closure releases its execution-local buffers; retained batch views copy decimal values/validity when ownership requires it. | Cache retention, output copying, cumulative allocated bytes and live reservations are separate observations. Existing value-copy ownership is tested; a copy's existence does not prove redundancy. |

Decimal native wire frames are outside P5. `plan_measure_pushdown.go:58` rejects
computed aggregate input recipes from native reduction; the decimal input is
such a recipe. The historical integer/native route must not be presumed equal
to the decimal local route. Current harness counters should identify both paths.

## Independent answers and semantic checks

The generated thirteen-row fixture contains ten coefficients999999999999999999
in group a, coefficients−999999999999999999 and−1 in group b, and a NULL in
group c. At scale10 the independent answers are:

| Scope | Exact output |
|---|---|
| Group a (ten maximum18-digit source values) | `999999999.9999999990` |
| Group b | `-100000000.0000000000` |
| Group c | NULL |
| All groups | `899999999.9999999990` |

Group a's coefficient9999999999999999990 exceeds int64 maximum. This is not
claimed to be ten maximum int64 inputs. [oracle.py](oracle.py) recomputes the
answers using Python arbitrary-precision integers and Decimal, independently
of engine query results; [independent-oracle.json](independent-oracle.json)
retains its concrete inputs and answers. Existing generated expectations are
literal PostgreSQL numeric answers matching this arithmetic.

Fifteen top-level focused tests passed with querymetrics:

```sh
go test -tags=querymetrics ./pkg/decimal ./pkg/query/ql ./pkg/bydbql -run 'Test(Int128|CompareScaled|PrecisionLimit|Decimal|CopyDecimal|ScaledDecimal|MeasureScaledDecimal)' -count=1 -v
```

[source-tests.txt](source-tests.txt) retains output. Tests cover signed values,
NULL/COALESCE, typed view copying, scale comparisons and mixed integer/decimal
IN/NOT IN operand ordering, exact state and checked precision38 overflow,
scale bounds, budgets and cancellation cleanup. `TestInt128Oracle` uses seeded
math/big integers/rationals over128 trials and up to110 steps each to independently
check addition, scale comparisons and formatting, including overflow. This tests
the precision38 arithmetic boundary without pretending a small RPC fixture can
accumulate10^38 from a few int64 inputs.

Generated real RPC qualification uses both standalone and distributed integration
suites sequentially with `-ginkgo.label-filter=QLB-T07 -ginkgo.focus=qg3_` and
fail-on-empty. The26 generated families exercise three bindings and both normal
and bypass caches; three additional cases require rejection of input arithmetic
overflow, ordinary int64 SUM overflow and invalid scale. The case named
`qg3_decimal_cancellation` means arithmetic cancellation (`v-v`), not context
cancellation; context cancellation is covered by the focused ownership tests.
The generated fixtures use the integration suite's existing replicated group
and normal routing, not the64/8192-row performance fixture. Each success checks
independent values, consumed rows/group cardinalities and restored ownership.

No new semantic matrix, performance run or production edits were added by this
review. Exact commands, build/source/generated/binary hashes and compressed raw
logs/reports are retained by [verify_semantics.py](verify_semantics.py).

Current outcome: **29/29 actual It specs passed in each topology**:26 successful
scenario families ×3 bindings ×2 cache modes = **156 successful verification
RPCs per topology**, plus3 expected rejection RPCs each. Ginkgo suite lifecycle
nodes are excluded from counts. [qg3-correctness.json](qg3-correctness.json)
contains observed counts and exact provenance. Compressed raw hashes refer to
decompressed original bytes. All production/source hashes matched before and
after the two runs.
