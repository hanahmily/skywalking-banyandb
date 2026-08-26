# Issue #13994 test ownership by internal TDD round

The HTML design keeps 86 traceability IDs as the cumulative acceptance matrix.
Some rows deliberately summarize several generated methods. This file assigns
each subcase to the internal TDD round that first makes it pass inside W-PR1,
W-PR2, or W-PR3. A round is a checklist item, not an issue or PR.

- W-PR1 contains A1–A4 and D1–D4.
- W-PR2 contains B1–B10.
- W-PR3 contains C1–C8.
- Direct verification is incremental: E1 belongs to W-PR1, E2 to W-PR2, and
  E3–E8 to W-PR3. Thus every workflow PR owns direct E2E proof.

## Unit tests — 33 IDs

| Design ID | Owning internal round(s) | Same-PR split rule |
|---|---|---|
| `U-CFG-01` | A2 | Users-only snapshot compatibility. |
| `U-CFG-02` | A3 | Built-in roles/bindings and immutable grants. |
| `U-CFG-03` | A3 | Enabled RBAC requires usable authentication. |
| `U-CFG-04` | A4 | Unknown principal, role, or permission references. |
| `U-CFG-05` | A4 | Duplicates and built-in override rejection. |
| `U-CFG-06` | A4, B1 | A4 owns cluster/global scope validation; B1 owns exact group semantics. |
| `U-CFG-07` | A2 | Existing owner-only file behavior. |
| `U-CFG-08` | A4 | Strict custom-role/RBAC fields. |
| `U-RLD-01` | A2, A4 | A2 owns atomic credentials; A4 owns atomic grants/revision. |
| `U-RLD-02` | A2, A4 | Both publication stages retain their last-known-good snapshot. |
| `U-RLD-03` | A2, A4 | A2 owns snapshot race safety; A4 adds decision concurrency. |
| `U-EVL-01` | A3 | Enabled RBAC default deny. |
| `U-EVL-02` | B1 | Exact alpha/beta scope. |
| `U-EVL-03` | B1 | Wildcard group behavior, including future names. |
| `U-EVL-04` | A4 | Additive custom/global binding union. |
| `U-EVL-05` | A3 | No implicit action permission. |
| `U-EVL-06` | A3 | Global permissions require wildcard scope. |
| `U-EVL-07` | B1 | Delete/recreate exact-name semantics. |
| `U-EVL-08` | C1–C4 | Each repeated-group request adapter adds its literal subcase. |
| `U-EXT-01` | B1–B7, B9, C1–C7 | Each adapter subcycle adds only its request types to the cumulative extractor table. |
| `U-EXT-02` | B1–B7, B9, C1–C7 | Each adapter owns its nil/empty/wrong-type rows. |
| `U-EXT-03` | A1 | Exhaustive active method registry. |
| `U-INT-01` | A3 | Unary disabled, denied, allowed, and handler-count paths. |
| `U-INT-02` | A3 | Trusted principal and metadata spoofing. |
| `U-INT-03` | A3 | Authentication before validation/authorization precedence. |
| `U-STR-01` | C7 | Stream-open grant decision. |
| `U-STR-02` | C7 | Per-frame scope and no-handler-on-deny. |
| `U-STR-03` | C7 | Binding revocation affects the next frame; user deletion is not a separate established-stream contract. |
| `U-SPC-01` | B8 | Group List post-handler filtering. |
| `U-SPC-02` | C8 | ByDBQL transformed-request decision. |
| `U-OBS-01` | A3, A4, C7, C8 | Each emitting lifecycle owns its bounded metric subcase; there is no cross-cutting metrics PR. |
| `U-OBS-02` | A3, A4, C8 | Only new RBAC/auth/reload output is checked; existing ByDBQL query logging is unchanged. |
| `U-FUZ-01` | A4 and each adapter cycle | A4 owns YAML/compiler fuzzing; adapter subcycles add their typed corpus inside the owning workflow PR. |

## Integration tests — 37 IDs

| Design ID | Owning internal round(s) | Same-PR split rule |
|---|---|---|
| `I-BOOT-01` | A2, A3 | A2 users-only/no-file compatibility; A3 explicit RBAC-disabled compatibility. |
| `I-BOOT-02` | A3 | Enabled unbound user default deny. |
| `I-AUT-01` | A3 | Direct gRPC credential/authorization precedence. |
| `I-AUT-02` | A3 | HTTP 400/401/403 distinction. |
| `I-AUT-03` | A3 | Gateway metadata spoof prevention. |
| `I-ROL-01` | B1–B10, C1–C8, D1–D4 | Each behavior subcycle adds only its cumulative reader rows. |
| `I-ROL-02` | B1–B10, C1–C8, D1–D4 | Each behavior subcycle adds only its cumulative writer rows. |
| `I-ROL-03` | A3 plus B/C/D leaves | A3 establishes admin semantics; every activated method appends its literal row. Unimplemented handler outcomes remain unchanged. |
| `I-ROL-04` | A4, D1 | A4 creates monitor; D1 activates its remaining cluster-read methods. |
| `I-SCH-01` | B4–B7 | Split by List, Get/Exist, Create/Update, and Delete extractor subcycles inside W-PR2. |
| `I-SCH-02` | B4–B7 | Same split for HTTP-bound registry methods. |
| `I-SCH-03` | B2, B3 | Group upsert and deletion are separate mutation subcycles inside W-PR2. |
| `I-DAT-01` | C1–C4 | Split by Stream, Measure/TopN, Trace, and Property fixture families. |
| `I-DAT-02` | C5, C6 | Apply and Delete have independent side-effect oracles. |
| `I-DAT-03` | C7 | One common stream lifecycle with literal protocol cases. |
| `I-SCP-01` | B/C adapter leaves | Each leaf adds alpha/beta cases only for its extractor. |
| `I-SCP-02` | B8 | Group List contents. |
| `I-SCP-03` | C1–C4 | Each repeated-group data read owns its mixed-scope case. |
| `I-SCP-04` | B1, B5 | Group and registry point-read existence precedence. |
| `I-SCP-05` | A4, B1 | Reloaded wildcard/exact-name semantics through Group reads. |
| `I-STR-01` | C7 | Allowed frame then forbidden frame for all three protocols. |
| `I-STR-02` | C7 | Binding removal while the stream is open. |
| `I-BQL-01` | C8 | Transformed Stream/Measure/Trace/Property/TopN table; one real fixture family. |
| `I-BQL-02` | C8 | Forbidden/parameterized/multi-group post-transform denial. |
| `I-ADM-01` | D2 | Real Snapshot side-effect boundary only. |
| `I-ADM-02` | A3, D1 | A3 owns ClusterState; D1 owns NodeQuery and Group Inspect/task Query. |
| `I-INT-01` | D4 | Conditional NodeSchemaStatus real handler. |
| `I-INT-02` | D3 | Admin gets existing `Unimplemented`; non-admin gets `PermissionDenied`. |
| `I-BAR-01` | B9, B10 | Key waits and global revision wait are different scope subcycles inside W-PR2. |
| `I-RLD-01` | A4 | Grant/revoke through live unary decision. |
| `I-RLD-02` | A4 | Invalid policy retains last known good. |
| `I-RLD-03` | A4 | Concurrent unary calls see one complete revision. |
| `I-HTP-01` | Every HTTP-bound B/C/D round | Each behavior round appends only its bindings; there is no parity-only issue or PR. |
| `I-HTP-02` | A3 | Authoritative gRPC decision counts a gateway request once. |
| `I-OBS-01` | A3, A4, C7, C8 | Metrics/redaction subcases ship with the emitting lifecycle. |
| `I-HLT-01` | A3 | Existing health policy in RBAC-on/off modes. |
| `I-DST-01` | W-PR3/E6 | Distributed verification repeats already-integrated public decisions and checks internal health. |

## Direct E2E tests — 16 IDs

| Design ID | Owning workflow stage | Same-PR boundary |
|---|---|---|
| `E-DIR-01` | W-PR2/E2 + W-PR3/E3 | W-PR2 owns schema bootstrap/barrier; W-PR3 adds protected marker seeding. |
| `E-DIR-02` | W-PR1/E1 + W-PR2/E2 + W-PR3/E3 | Each PR appends only its global, schema, or data direct-gRPC role rows. |
| `E-DIR-03` | W-PR1/E1 + W-PR2/E2 + W-PR3/E3 | Each PR appends only its equivalent HTTP rows. |
| `E-DIR-04` | W-PR2/E2 + W-PR3/E3 | W-PR2 owns filtered Group.List; W-PR3 owns native mixed-scope query. |
| `E-DIR-05` | W-PR3/E3 | Three bidirectional stream protocols. |
| `E-DIR-06` | W-PR3/E4 | ByDBQL transformed-scope journey. |
| `E-DIR-07` | W-PR1/E1 + W-PR3/E5 | W-PR1 owns unary revocation; W-PR3 adds established-stream revocation. |
| `E-DIR-08` | W-PR1/E1 | Malformed reload last-known-good behavior. |
| `E-DIR-09` | W-PR1/E1 + W-PR3/E5 | W-PR1 owns unary/reload metrics; W-PR3 adds stream/ByDBQL rows. |
| `E-DIR-10` | W-PR1/E1 | Gateway metadata spoof attempt. |
| `E-DST-01` | W-PR3/E6 | Two-liaison decision consistency. |
| `E-DST-02` | W-PR3/E6 | Cross-liaison write/query and schema barrier. |
| `E-DST-03` | W-PR3/E7 | Distributed concurrent reload stress. |
| `E-DST-04` | W-PR3/E7 | Liaison restart/readiness stress. |
| `E-CMP-01` | W-PR1/E1 | No-auth-file direct compatibility; users-only/disabled stay integration tests. |
| `E-CMP-02` | W-PR1/E1 | Separate negative-start script, outside normal Compose readiness. |

## Verification and docs stages

- **E1 / W-PR1:** create the standalone direct case and prove
  compatibility/bootstrap, global roles/transports, unary reload/metrics, and
  spoof resistance.
- **E2 / W-PR2:** add schema bootstrap, schema role/transport rows, filtered
  Group.List, and SchemaBarrier.
- **E3 / W-PR3:** add native data role/transport rows, mixed-scope queries, and
  streaming journeys.
- **E4:** add ByDBQL journey only.
- **E5:** add reload/metrics journey only; each script changes and restores
  its own fixture.
- **E6:** create the separate distributed direct case and required smoke.
- **E7:** add non-blocking nightly reload/restart stress only.
- **E8:** operator/security docs and examples only.

E1–E8 are checklist stages inside their named workflow PR. They are not GitHub
issues and do not own PRs.

No OAP or Canopy E2E is in this ownership map.
