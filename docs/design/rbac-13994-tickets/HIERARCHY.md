# Issue #13994 three-sub-issue hierarchy

[Apache SkyWalking #13994](https://github.com/apache/skywalking/issues/13994)
has exactly three sub-issues. Each sub-issue owns one complete TDD workflow,
one direct E2E stage, and one PR. The A–D identifiers below are internal
checklist rounds, not GitHub issues.

## [W-PR1 #14014 — security runtime and global authorization](https://github.com/apache/skywalking/issues/14014)

Owns PR 1 and eight sequential internal rounds:

1. `A1` — method-policy startup invariant.
2. `A2` — immutable credential snapshots through Basic auth.
3. `A3` — built-in RBAC on `ClusterState/GetClusterState`.
4. `A4` — flat custom roles and atomic policy reload.
5. `D1` — remaining cluster-read method table.
6. `D2` — real Snapshot admin boundary.
7. `D3` — authorization precedence for unimplemented public methods.
8. `D4` — conditional NodeSchemaStatus admin boundary.

Merge gate: runtime/global integration and the direct global/bootstrap E2E
stage are GREEN; schema and data methods remain fail-closed.

## [W-PR2 #14015 — group-scoped schema authorization](https://github.com/apache/skywalking/issues/14015)

Blocked until #14014 merges to `main`. Owns PR 2 and ten sequential internal
rounds:

1. `B1` — Group point reads.
2. `B2` — Group create/update by `Metadata.Name`.
3. `B3` — Group deletion before task creation.
4. `B4` — registry List by direct group.
5. `B5` — registry Get/Exist by metadata group.
6. `B6` — registry Create/Update resource bodies.
7. `B7` — registry Delete by metadata group.
8. `B8` — `Group.List` response filtering.
9. `B9` — SchemaBarrier key waits.
10. `B10` — wildcard schema read for revision waits.

Merge gate: Group, all seven registries, filtered `Group.List`, SchemaBarrier,
and the direct schema E2E stage are GREEN; data methods remain fail-closed.

## [W-PR3 #14016 — data, special paths, and release proof](https://github.com/apache/skywalking/issues/14016)

Blocked until #14015 merges to `main`. Owns PR 3 and eight sequential internal
rounds:

1. `C1` — Stream queries by every requested group.
2. `C2` — Measure Query/TopN by every requested group.
3. `C3` — Trace queries by every requested group.
4. `C4` — Property queries by every requested group.
5. `C5` — Property Apply without denied side effects.
6. `C6` — Property Delete without denied side effects.
7. `C7` — per-frame Measure/Stream/Trace write authorization.
8. `C8` — ByDBQL authorization after native-request transformation.

Merge gate: native data, Property mutations, streaming frames, ByDBQL,
standalone/distributed direct E2E, metrics proof, and documentation are GREEN.

## Execution

```text
#14014 merge → #14015 RED begins
#14015 merge → #14016 RED begins
#14016 merge → #13994 complete
```

Within a workflow PR:

1. Add the next round public-behavior test and record RED.
2. Add only the minimum production activation and record GREEN.
3. Run the focused real-liaison integration subcase.
4. Refactor while the round and cumulative tests stay GREEN.
5. Update the workflow issue and PR checklist, then begin the next round.
6. After all rounds pass, run the workflow integration and direct E2E gates.

Do not open stacked workflow PRs or batch every RED before implementation.
Do not create an issue or PR for an internal round. If a round needs a smaller
boundary, split its checklist inside the same workflow issue and PR.
