# Issue #13994 internal TDD-round catalog

The three workflow issues are filed in the [live hierarchy](HIERARCHY.md).
The A–D entries below are internal checklist rounds within those workflows.

Every A–D entry is one vertical RED→GREEN cycle inside its owning workflow PR.
It does **not** own an issue or independent PR.

| Owning workflow PR | Internal TDD rounds |
|---|---|
| W-PR1 — security runtime/global | A1–A4, D1–D4 |
| W-PR2 — schema | B1–B10 |
| W-PR3 — data/special | C1–C8 |

## Non-negotiable subcycle contract

Each internal round has:

1. one lifecycle boundary: startup, unary pre-handler, unary post-filter,
   stream `RecvMsg`, ByDBQL post-transform, or snapshot publication;
2. normally one permission and one scope/extractor family;
3. no more than four observable requirements;
4. one named RED test that fails before this cycle's GREEN change;
5. one focused real-liaison integration scenario added to the owning PR during
   this cycle;
6. one independent fixture/oracle family; and
7. one RED→GREEN checklist cycle, but no separate issue or PR.

gRPC and grpc-gateway HTTP are transport proofs of the same boundary, not two
authorization implementations. Helpers and context values must be consumed by
the named production path during the same cycle.

A3 establishes fail-closed rollout inside W-PR1: when RBAC is enabled, any
permission-bearing method without an activated executor returns
`PermissionDenied` before its handler. Later cycles activate only their named
executor family. W-PR1 can therefore merge before W-PR2/W-PR3 without a bypass,
while RBAC remains disabled by default.

## A — W-PR1 authorization framework subcycles

### A1 — Reject an unclassified method before runtime starts

- **Boundary:** immutable `MethodPolicyRegistry.Validate` against
  `grpc.Server.GetServiceInfo()`.
- **Production activation:** `server.Serve` registers and validates before TLS
  or auth watchers, repair queues, goroutines, and `net.Listen`.
- **Requirements:** fixed 78-method oracle; deterministic missing/stale/duplicate
  errors; four conditional-service combinations; zero runtime work on failure.
- **RED:** `TestServerRejectsUnclassifiedMethodBeforeRuntimeStart`; current
  `main` has no registry and proceeds toward runtime startup.
- **Integration:** a real server with one rule removed leaves its port unbound
  and starts no watcher or repair queue.
- **Cycle entry:** W-PR1 entry; first internal round.
- **Files:** `banyand/liaison/grpc/server.go`, focused method-policy files/tests.

### A2 — Publish immutable credential snapshots through Basic auth

- **Boundary:** `auth.Reloader.Current()` returns an immutable snapshot;
  initial load/reload builds one candidate and atomically publishes it.
- **Production activation:** existing gRPC and HTTP Basic authentication read
  credentials through the snapshot-backed auth API.
- **Requirements:** retained snapshots never mutate; valid reload publishes
  exactly once; invalid/unsafe reload retains snapshot and hash; disabled,
  health-auth, users-only, and `0600` behavior remains compatible.
- **RED:** `TestReloaderPublishesImmutableCredentialSnapshot`; the seam is
  absent and current retained `*Config` values alias field-wise mutations.
- **Integration:** one live authenticated standalone connection transitions
  Alice to Bob after rewrite; malformed YAML leaves Bob usable.
- **Cycle entry:** A1 GREEN in W-PR1.
- **Files:** `banyand/liaison/pkg/auth`, compatibility adapters in gRPC/HTTP
  auth, existing standalone authentication integration.

### A3 — Enforce built-in RBAC on `ClusterState/GetClusterState`

- **Boundary:** one compiled `Decide(principal, policy, scopes)` call consumed
  by the unary interceptor for `GetClusterState`.
- **Production activation:** authentication places the selected snapshot and
  trusted principal in context; the real unary chain authorizes this method.
- **Requirements:** users-only/RBAC-off compatibility plus strict built-in
  binding YAML; authentication/validation/authorization precedence; global
  admin allowed while reader/writer/unbound deny without handler invocation;
  all not-yet-activated permission methods fail closed.
- **RED:** `TestRBACGetClusterStateDeniesUnboundPrincipal`; after A1 and A2 are GREEN, the
  legacy authenticated caller is still allowed.
- **Integration:** bad credentials are `Unauthenticated`; reader/unbound are
  `PermissionDenied`; built-in admin receives the real cluster-state response
  over gRPC and HTTP; forged gateway identity cannot replace Basic auth.
- **Observability owned here:** the bounded unary decision counter, secret-safe
  errors, and exactly one observation for an HTTP gateway call.
- **Cycle entry:** A1 and A2 GREEN in W-PR1.
- **Files:** RBAC model/compiler/authorizer, auth snapshot/context, unary rules,
  gRPC server chain, standalone RBAC suite.

### A4 — Resolve and reload flat custom roles through cluster state

- **Boundary:** extend the same snapshot compiler and live decision API; no
  second authorizer or publication path.
- **Production activation:** `GetClusterState` consumes the custom role after
  the existing reloader atomically publishes it.
- **Requirements:** only the six fixed permissions; reject duplicates,
  built-in overrides, unknown fields/permissions, and invalid cluster scopes;
  invalid reload remains last-known-good; custom `monitor:cluster:read` on `*`
  is allowed after reload.
- **RED:** `TestCustomRoleAuthorizesClusterState`; A3 rejects `roles` or keeps
  the monitor denied.
- **Integration:** rewrite built-in-only YAML to a monitor binding without
  restart, then write an invalid revision; the valid monitor decision remains.
- **Observability owned here:** reload success/failure counters, active revision
  gauge, and secret-safe reload errors.
- **Cycle entry:** A3 GREEN in W-PR1.
- **Files:** RBAC compiler, auth snapshot/reloader tests, standalone cluster
  state reload scenario.

## D — W-PR1 global cluster/admin subcycles

### D1 — Authorize the remaining cluster-read method table

- **Boundary/callers:** one global `cluster:read` executor table for
  `NodeQuery.GetCurrentNode`, Group `Inspect`, and deletion-task `Query`.
- **RED:** `TestRBACClusterReadMethodsRequireGlobalGrant`; A3 fail-closes a
  custom monitor on these methods.
- **Integration:** monitor/admin reach one real representative handler;
  reader/writer/exact-scope cluster grant deny. Literal unit rows cover all
  three methods and gRPC/HTTP bindings.
- **Cycle entry:** A4 GREEN in W-PR1.

### D2 — Authorize real Snapshot administration

- **Boundary/caller:** one global `cluster:admin` executor for
  `SnapshotService.Snapshot`.
- **RED:** `TestRBACSnapshotRequiresClusterAdmin`.
- **Integration:** admin reaches the real snapshot handler; monitor/writer/reader
  deny without a snapshot side effect over gRPC/HTTP.
- **Cycle entry:** D1 GREEN in W-PR1.

### D3 — Preserve authorization precedence for unimplemented public methods

- **Boundary/callers:** one global-admin executor family for Measure
  `InternalQuery` and Stream/Measure/Trace `DeleteExpiredSegments` on the public
  liaison listener.
- **RED:** `TestRBACUnimplementedMethodsAuthorizeBeforeDispatch`; A3 fail-closes
  admin as well as non-admin because the family is not activated.
- **Integration:** admin passes RBAC then receives the existing gRPC
  `Unimplemented`; other roles receive `PermissionDenied`, proving no handler or
  maintenance side effect was invented for #13994.
- **Cycle entry:** D2 GREEN in W-PR1.

### D4 — Authorize conditional NodeSchemaStatus methods

- **Boundary/callers:** one conditional generated-service executor table for
  its three methods, requiring global `cluster:admin`.
- **RED:** `TestRBACNodeSchemaStatusRequiresClusterAdmin`.
- **Integration:** with the conditional service present, admin reaches the real
  handler and other roles deny; service absence remains a valid registry state.
- **Cycle entry:** D3 GREEN in W-PR1.

## B — W-PR2 group-scoped schema subcycles

All B rounds activate explicit full-method executor bindings in A3's unary
authorizer. They do not modify metadata repositories or generated protobufs.
Fixtures are seeded outside the protected caller, so no B round has an
artificial dependency on another B round.

### B1 — Authorize Group point reads

- **Boundary/callers:** typed direct-group extractor for only Group `Get` and
  `Exist`.
- **Requirements:** exact/wildcard allow; wrong scope denies before lookup;
  unauthorized `Exist` returns `PermissionDenied`, not false; malformed group
  returns `InvalidArgument` after authentication.
- **RED:** `TestRBACGroupPointReadUsesRequestGroup`; A3 still fail-closes the
  permitted alpha reader.
- **Integration:** preloaded alpha/beta; alpha reader gets/exists alpha and is
  denied beta, while wildcard sees both, over gRPC and HTTP where bound.
- **Cycle entry:** W-PR1 merged; first W-PR2 internal round.

### B2 — Authorize Group create/update by `Group.Metadata.Name`

- **Boundary/callers:** group-body-name extractor activates only Group `Create`
  and `Update`; it does not read `Metadata.Group`.
- **Requirements:** exact/wildcard writer allowed; reader denied; nil/empty body
  invalid; denial precedes metadata mutation.
- **RED:** `TestRBACGroupUpsertUsesMetadataName`; A3 fail-closes the allowed
  writer.
- **Integration:** exact writer creates/updates its bound not-yet-existing
  group; reader denial followed by writer success proves no side effect.
- **Cycle entry:** B1 GREEN in W-PR2.

### B3 — Authorize Group deletion before task creation

- **Boundary/caller:** direct-group write executor activates only Group
  `Delete`, before dry-run, force, or deletion-task logic.
- **Requirements:** writer allowed; reader denied for every flag combination;
  denial does not disclose existence; no task/deletion starts on denial.
- **RED:** `TestRBACGroupDeleteRunsBeforeDeletionTask`.
- **Integration:** deny reader deletion of alpha, then let the exact writer
  delete the same group successfully.
- **Cycle entry:** B2 GREEN in W-PR2.

### B4 — Authorize registry List by direct group

- **Boundary/callers:** structural `GetGroup() string` extractor activates the
  seven registry `List` methods.
- **Requirements:** literal seven-method oracle; exact/wildcard allow; wrong
  group denies before handlers; empty group is invalid.
- **RED:** `TestRBACRegistryListUsesDirectGroup`; alpha Lists are fail-closed on
  A3.
- **Integration:** a seven-client List matrix allows alpha and denies beta.
- **Cycle entry:** B3 GREEN in W-PR2.

### B5 — Authorize registry Get/Exist by metadata group

- **Boundary/callers:** structural `GetMetadata() *commonv1.Metadata` extractor
  activates seven `Get` and seven `Exist` methods.
- **Requirements:** exact/wildcard allow; nil/empty metadata invalid; wrong
  scope denies before lookup; unauthorized `Exist` never returns false.
- **RED:** `TestRBACRegistryPointReadUsesMetadataGroup`.
- **Integration:** real Measure Get/Exist proves alpha allow and beta deny; a
  literal unit table covers all seven generated request types.
- **Cycle entry:** B4 GREEN in W-PR2.

### B6 — Authorize registry Create/Update resource bodies

- **Boundary/callers:** typed resource adapters extract nested
  `Metadata.Group` and activate seven `Create` plus seven `Update` methods.
- **Requirements:** complete method/request oracle; writer exact/wildcard
  allowed; reader/nil body denied or invalid before handler; denial has no
  schema side effect.
- **RED:** `TestRBACRegistryUpsertUsesResourceMetadataGroup`.
- **Integration:** writer creates/updates an alpha Measure; reader's denied
  create followed by writer success proves absence of a side effect.
- **Cycle entry:** B5 GREEN in W-PR2.

### B7 — Authorize registry Delete by metadata group

- **Boundary/callers:** the metadata-group write executor activates the seven
  registry `Delete` methods.
- **Requirements:** exact/wildcard writer allow; reader deny; malformed metadata
  invalid; denial precedes tombstone/storage effects.
- **RED:** `TestRBACRegistryDeleteUsesMetadataGroup`.
- **Integration:** deny reader deletion of a Measure, then the writer receives
  `deleted=true` for that same resource.
- **Cycle entry:** B6 GREEN in W-PR2.

### B8 — Filter `Group.List` with the request snapshot

- **Boundary/caller:** one post-handler response transformer selected only for
  Group `List`; it uses the same snapshot selected for the request decision.
- **Requirements:** no schema-read grant denies before handler; exact scopes
  filter; wildcard returns all; reload during the handler cannot mix snapshots.
- **RED:** `TestRBACGroupListFiltersVisibleGroups`; A3 either fail-closes the
  allowed reader or an unfiltered handler returns alpha and beta.
- **Integration:** exact reader sees alpha only, wildcard sees alpha/beta, and a
  no-read principal is denied over gRPC and HTTP.
- **Cycle entry:** B7 GREEN in W-PR2.

### B9 — Authorize SchemaBarrier key waits for every group

- **Boundary/callers:** typed repeated-key extractor activates
  `AwaitSchemaApplied` and `AwaitSchemaDeleted`; a `kind=group` key uses
  `SchemaKey.Name`, while other keys use `SchemaKey.Group`.
- **Requirements:** deduplicate scopes; require all scopes; handle group-kind
  keys; deny before cache polling or cluster fan-out.
- **RED:** `TestRBACSchemaBarrierKeyWaitsRequireEveryScope`.
- **Integration:** alpha reader may await alpha, is denied alpha+beta, and a
  wildcard reader may await both; both methods are table-covered.
- **Cycle entry:** B8 GREEN in W-PR2.

### B10 — Require wildcard schema read for revision waits

- **Boundary/caller:** the existing global-scope decision is bound only to
  `AwaitRevisionApplied`; no group is inferred from the request.
- **Requirements:** exact reader denied; wildcard reader/writer/admin allowed by
  permission; denial precedes fan-out; revision zero remains a real allow case.
- **RED:** `TestRBACSchemaBarrierRevisionRequiresWildcard`.
- **Integration:** alpha-only reader is denied and wildcard reader gets
  `applied=true` for revision zero.
- **Cycle entry:** B9 GREEN in W-PR2.

## C — W-PR3 data and special-path subcycles

### C1 — Authorize Stream queries by all requested groups

- **Boundary/caller:** typed repeated-group executor for `StreamService.Query`.
- **RED:** `TestRBACStreamQueryRequiresEveryGroup`; A3 fail-closes an allowed
  alpha reader.
- **Integration:** alpha marker succeeds; beta and alpha+beta deny with no
  partial result over gRPC and HTTP.
- **Cycle entry:** W-PR2 merged; first W-PR3 internal round.

### C2 — Authorize Measure Query and TopN by all groups

- **Boundary/callers:** one Measure data-read executor family for the repeated
  `groups` field on `Query` and `TopN`.
- **RED:** `TestRBACMeasureReadsRequireEveryGroup`.
- **Integration:** one Measure fixture proves Query and TopN alpha allow and
  beta/mixed all-or-nothing denial over gRPC and HTTP.
- **Cycle entry:** C1 GREEN in W-PR3.

### C3 — Authorize Trace queries by all requested groups

- **Boundary/caller:** typed repeated-group executor for `TraceService.Query`.
- **RED:** `TestRBACTraceQueryRequiresEveryGroup`.
- **Integration:** alpha trace succeeds; beta and mixed requests deny without
  span leakage over gRPC and HTTP.
- **Cycle entry:** C2 GREEN in W-PR3.

### C4 — Authorize Property queries by all requested groups

- **Boundary/caller:** typed repeated-group executor for
  `PropertyService.Query`.
- **RED:** `TestRBACPropertyQueryRequiresEveryGroup`.
- **Integration:** alpha property succeeds; beta and mixed requests deny
  without leakage over gRPC and HTTP.
- **Cycle entry:** C3 GREEN in W-PR3.

### C5 — Authorize Property Apply without denied side effects

- **Boundary/caller:** nested property-metadata executor for
  `PropertyService.Apply`.
- **RED:** `TestRBACPropertyApplyUsesPropertyGroup`.
- **Integration:** alpha writer applies; reader/beta deny, and an admin query
  proves the property was not created or changed.
- **Cycle entry:** C4 GREEN in W-PR3.

### C6 — Authorize Property Delete without denied side effects

- **Boundary/caller:** direct-group executor for `PropertyService.Delete`.
- **RED:** `TestRBACPropertyDeleteUsesRequestGroup`.
- **Integration:** alpha writer deletes; reader/beta deny and the same property
  remains present.
- **Cycle entry:** C5 GREEN in W-PR3.

### C7 — Authorize every resource-bearing write-stream frame

- **Boundary/callers:** one common `ServerStream.RecvMsg` lifecycle plus typed
  Measure, Stream, and Trace frame adapters; no handler is modified.
- **Requirements:** principal without any `data:write` grant rejects at open;
  every resource-bearing frame uses its resolved metadata group; binding reload
  affects the next frame; denied frames never reach storage.
- **RED:** `TestRBACStreamFrameUsesCurrentSnapshot`; on A4 an allowed stream is
  still fail-closed and no frame adapter exists.
- **Integration:** real Measure/Stream/Trace bidi clients send allowed alpha then
  forbidden beta; each stream ends `PermissionDenied` and beta markers are absent.
- **Observability owned here:** one bounded decision per resource-bearing frame;
  no principal/group labels.
- **Cycle entry:** C6 GREEN in W-PR3; A4 is already merged through W-PR1.

### C8 — Authorize ByDBQL after native-request transformation

- **Boundary/caller:** one post-transform/pre-dispatch gate inside the single
  `BydbQLService.Query` handler; raw SQL and HTTP verb are not resources.
- **Requirements:** authorize groups from each transformed native request type;
  one forbidden group denies the whole query; denial precedes native handler;
  parameters cannot bypass the transformed decision.
- **RED:** `TestRBACByDBQLAuthorizesTransformedRequest`; native query interceptors
  are bypassed by the current direct handler dispatch.
- **Integration:** gRPC/HTTP Measure queries prove alpha allow, beta/mixed deny,
  and a table unit test covers Stream/Trace/Property/TopN transformations.
- **Observability owned here:** one bounded ByDBQL decision, without raw query in
  new RBAC logs or labels.
- **Cycle entry:** C7 and C1–C4 GREEN in W-PR3.

## Cycle-start audit

A boundary above is a maximum, not permission to grow a round. Before starting
it, confirm the previous round is GREEN and the named seam still matches the
current workflow branch. If repository evidence shows two production
activations, fixture/oracle families, or independent RED→GREEN slices, split
the checklist inside the **same workflow issue and PR**. Do not create another
issue, a fourth workflow, or an independent round PR.
