# Decisions — Architecture Decision Records

This document records significant design decisions. Add new ADRs at the bottom.
Never edit a merged ADR — supersede it with a new one if needed.

## ADR-001: Static plugin registration via underscore imports

**Status:** Accepted | **Date:** 2026-03-09

**Context:** Plugins extend the operator with new action types. Options: static
compile-time, Go plugins, WASM, gRPC subprocess.

**Decision:** Static compile-time registration via underscore imports in
`cmd/operator/main.go`. Each plugin's `init()` calls `action.Register()`.

**Consequences:** Simplest implementation, type-safe, no runtime overhead, easy
RBAC aggregation. Trade-off: adding a plugin requires rebuilding the operator.

**Alternatives:** Go `plugin` package (fragile, build-env dependent), gRPC
subprocess (operational complexity), WASM (tooling immature for v0.1).

---

## ADR-002: No AI/LLM integration in v0.1

**Status:** Accepted | **Date:** 2026-03-09

**Context:** Project vision includes AI-assisted decisions, but 2026 SRE tooling
is saturated with AI products and fatigue is real.

**Decision:** v0.1 has zero LLM integration. No `aiConsult` field. No external
HTTP except to configured metric source.

**Consequences:** Stands on its own merits. Easier in regulated environments.
Faster delivery. Differentiation. AI can be added in v0.3 as opt-in layer.

**Alternatives:** Ship AI in v0.1 (slows delivery). Ship a stub field (worse than
nothing).

---

## ADR-003: ActionAudit records as a separate CRD

**Status:** Accepted | **Date:** 2026-03-13

**Context:** Every action execution must be auditable. Options: policy status
field, annotations on targets, dedicated CRD, external database.

**Decision:** Dedicated `ActionAudit` CRD. Operator garbage-collects per the
policy's `audit.retention` (default 30d).

**Consequences:** First-class K8s objects, queryable, exportable, decoupled
from policy lifecycle. Trade-off: etcd consumption can grow.

**Alternatives:** Status field (size limits, lost on delete), annotations (no
structured data), database (operational nightmare).

---

## ADR-004: CRD name is `ReactivePolicy`, not `MetricAction`

**Status:** Accepted | **Date:** 2026-03-13

**Context:** Naming the CRD shapes user mental model.

**Decision:** `ReactivePolicy`. "Policy" matches K8s vocabulary. "Reactive"
captures intent. Short names `rp`, `rpolicy` available. Doesn't lock the project into
metrics-only signals.

**Alternatives:** `MetricAction` (too narrow), `ObservedPolicy` (vaguer),
`ConditionalTrigger` (sounds like workflow engine).

---

## ADR-005: v0.1 supports Prometheus only

**Status:** Accepted | **Date:** 2026-03-18

**Context:** Many possible metric sources. Supporting all in v0.1 unrealistic.

**Decision:** Prometheus only in v0.1. CRD enum allows only `prometheus`. v0.2
will add Loki for log-derived metrics.

**Consequences:** Sharp focus, covers 80% of CNCF audience, MetricSource
interface designed for extension. Trade-off: non-Prometheus shops can't adopt
v0.1.

---

## ADR-006: Pluggable actions, sequential execution, configurable failure handling

**Status:** Accepted | **Date:** 2026-03-21

**Context:** Need to define how the `actions` list is processed.

**Decision:** Sequential execution in declared order. `onFailure: continue |
stop | rollback` per action. Default `stop`. `rollback` invokes `Reverse()` on
preceding succeeded actions in reverse.

**Consequences:** Predictable, models dependencies naturally, rollback is safe.
Trade-off: slower than parallel. Acceptable since action count per policy is small.

**Alternatives:** Parallel (unclear ordering), DAG (overkill — use Argo Workflows).

---

## ADR-007: Cooldown and rate limit are non-negotiable

**Status:** Accepted | **Date:** 2026-03-26

**Context:** Bug in a policy or flapping metric could cause runaway action
execution.

**Decision:** Every policy has `cooldown` (default 5m, min 30s, max 24h) and
`maxTriggersPerHour` (default 5, min 1, max 60). Defaults apply if omitted.
Cannot be disabled. ValidatingWebhook enforces bounds.

**Consequences:** Runaway policy bug class impossible by design. Predictable.
Trade-off: high-frequency reactive use cases (per-minute scaling) not supported —
those belong to KEDA.

---

## ADR-008: Apache 2.0 license

**Status:** Accepted | **Date:** 2026-03-26

**Context:** License affects adoption, contribution, future commercial.

**Decision:** Apache 2.0. Aligned with CNCF projects for potential Sandbox
application. Patent grant protects users.

**Alternatives:** MIT (no patent grant), GPL/AGPL (discourages enterprise).

---

## ADR-009: Build cooldown/rate-limit state from ActionAudit records

**Status:** Accepted | **Date:** 2026-04-02

**Context:** Where to store "has this policy triggered recently?" — in-memory
loses on restart, CRD status creates hot writes.

**Decision:** Query `ActionAudit` records on-demand. Cache last query for
`pollInterval` to avoid hammering etcd.

**Consequences:** Survives restarts, single source of truth, no hot writes.
Trade-off: extra LIST call per reconcile, mitigated by caching.

---

## ADR-010: Development happens inside an Ubuntu 24.04 VM

**Status:** Accepted | **Date:** 2026-04-07

**Context:** Operator development needs a reproducible Linux environment with
`kind`, plus the freedom to run long automated tasks without risking the host.

**Decision:** The standard dev environment is a local Ubuntu 24.04 VM (UTM/
VirtualBox/Parallels). A VM snapshot before any risky operation turns a
mistake into a 30-second rollback.

**Consequences:** Reproducible environment, host machine isolated, trivial
rollback. Trade-off: VM overhead (~8GB RAM, ~30GB disk on host).

**Alternatives:** Container (kind doesn't run nested well), bare metal (no easy
rollback), cloud VM ($5-20/month always-on).

---

## ADR-011: Human approval as a pre-execution gate on the audit record

**Status:** Accepted | **Date:** 2026-08-21

**Context:** Until v0.3 the safety model was entirely post-hoc: bound the blast
radius (sustained duration, cooldown, hourly cap, `maxResources`), make actions
reversible, and prove what happened afterwards. Reversibility did the job
approval would normally do — act now, undo cheaply, prove later. That argument
breaks down in two places: the `allowIrreversible` opt-in, and actions whose
blast radius is wide enough that you do not want to learn about them from the
audit trail. Quarantining a workload or shifting production traffic are not
things to discover after the fact.

**Decision:** An action may set `requiresApproval: true`. The pipeline runs
every action ahead of the gate at trigger time and stops there, writing its
`ActionAudit` **before** the gated action rather than after. The record is both
the approval token and the audit trail — there is no second store to query, and
the evidence an approver needs (metric value, actions already run, plugins held,
resolved targets) is on the object they are deciding about. `rp action approve`
or `deny` records a verdict; the operator then runs or skips the held actions
and folds the outcomes into the same record.

Four things fall out of that, each deliberate:

- **One gate per pipeline.** Two would mean a single trigger needs two separate
  decisions, and the actions between them would run on an approval nobody gave
  for them. The webhook rejects a second gate.
- **Identity comes from admission, not from the object.** Kubernetes does not
  persist who set a field, so if `decidedBy` were an ordinary field, anyone able
  to record a decision could also record whose it was. A mutating webhook stamps
  it from the authenticated request and overwrites what the client sent.
  Decisions are write-once.
- **Expiry denies.** An undecided gate lapses at `approvalTimeout` (default
  30m) and its actions never run.
- **Cooldown starts at the decision, not the trigger.** Waiting on a human is
  not the quiet period that is supposed to follow an action. While a gate is
  open the policy does not trigger again, so a metric that stays bad — which it
  will, that is why someone is being asked — cannot queue a second gate behind
  the first.

**Consequences:** Gated pipelines get a review step without a separate approval
service, and the audit answers "who approved this" as well as "what happened".
The resumed pipeline is confined to the targets resolved at trigger time, so an
approval cannot silently widen its own blast radius. Trade-offs: `ActionAudit`
is no longer written once and never touched — the operator completes a gated
record, which is why the webhook restricts what a client may change on it.
Rollback also no longer spans a whole trigger: actions released by an approval
cannot reverse actions audited as complete before the approver looked.

Execution still runs as the operator's own service account. Resolving it from
the approver's identity via impersonation — so RBAC *enforces* the approval
rather than merely recording it — is the natural next step and deliberately not
in this change: it needs the `impersonate` verb, which is worth introducing on
its own.

**Alternatives:** A separate `ApprovalRequest` CRD (two objects to keep in step,
and the audit still would not carry the decision). Gating the whole pipeline
rather than splitting it (simpler, but the Slack notification would wait behind
the approval, so nobody would know a gate was open). Trusting a `decidedBy`
field without a webhook (unforgeable only if nobody can write the object, which
defeats the point).

---

(Add new ADRs below this line.)
