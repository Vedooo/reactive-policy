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
captures intent. Short names `rp`, `rpolicy` available. Doesn't lock us into
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

(Add new ADRs below this line.)
