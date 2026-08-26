# Issue #13994 RBAC implementation plan

This directory supports the proposed [BanyanDB API RBAC design](../rbac-13994-review.html)
for [apache/skywalking#13994](https://github.com/apache/skywalking/issues/13994).
The GitHub issues are the implementation contracts; these files preserve the
cross-issue design, ordering, and test ownership without duplicating issue
bodies.

## Documents

- [Hierarchy](HIERARCHY.md): the three PR boundaries, live issue tree, and
  execution order.
- [Internal TDD-round catalog](SUB-TDD-CYCLES.md): the boundary, RED test, and focused
  integration proof for all 26 cycles.
- [Test ownership](TEST-OWNERSHIP.md): ownership of all 86 unit, integration,
  and direct E2E test IDs.

## Delivery model

| Workflow | Scope | Internal TDD rounds | Pull requests |
|---|---|---:|---:|
| [W-PR1 #14014](https://github.com/apache/skywalking/issues/14014) | Security runtime and global authorization | A1–A4, D1–D4 (8) | 1 |
| [W-PR2 #14015](https://github.com/apache/skywalking/issues/14015) | Group-scoped schema authorization | B1–B10 (10) | 1 |
| [W-PR3 #14016](https://github.com/apache/skywalking/issues/14016) | Data, special paths, release proof, and documentation | C1–C8 (8) | 1 |

```text
26 internal rounds = 26 strict RED → GREEN cycles
                   = 0 additional issues or pull requests

3 sub-issues       = 3 complete TDD workflows
                   = 3 pull requests total
```

W-PR1 must merge to `main` before W-PR2 begins. W-PR2 must merge before
W-PR3 begins. There are no stacked PRs.

## Non-negotiable workflow rules

1. Each workflow issue owns exactly one independently mergeable PR.
2. Each internal round owns one bounded RED→GREEN cycle on the workflow branch.
   It is a checklist item, not an issue or independent PR.
3. A cycle starts only after the previous cycle is GREEN on that branch.
4. Each cycle activates its named production caller and adds focused
   real-liaison integration proof in the same workflow PR.
5. Each workflow PR records a workflow-level RED, cumulative GREEN, direct E2E
   stage, and final refactor/test gate.
6. OAP and Canopy E2E are excluded. The tests call BanyanDB directly.

The three filed workflow issues carry the `database` label. No queue label is
prescribed by this design.
