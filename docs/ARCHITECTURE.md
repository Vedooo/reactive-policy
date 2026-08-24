# Architecture

This document describes how `reactive-policy` is structured internally. Read this
before implementing any cross-component change, or when a design question crosses
package boundaries.

## 1. Components

The project ships three logical components from a single repository and a single
Go module.

### 1.1 Operator (`cmd/operator`)

A standard Kubernetes controller built with Kubebuilder + controller-runtime. It
runs in-cluster (one replica with leader election, scaled to one) and is the only
component that mutates cluster state. Responsibilities:

- Watch `ReactivePolicy` resources
- Poll metric sources at the cadence defined by each policy
- Evaluate threshold expressions
- Execute action pipelines through the plugin registry
- Maintain `status` on each `ReactivePolicy`
- Write `ActionAudit` records (see ADR-003 in DECISIONS.md)
- Serve its own Prometheus `/metrics` endpoint
- Serve a ValidatingWebhook for policy admission

### 1.2 CLI (`cmd/cli`)

A standalone Go binary that talks to the cluster via the standard kubeconfig. It
does NOT talk to the operator directly — there is no RPC between them. Everything
the CLI shows is read from the Kubernetes API (CRD status, audit records, events).
This keeps the CLI stateless and trivially portable. Responsibilities:

- `policy list/get/describe` — show policies and their status
- `policy dry-run <file>` — simulate a policy against current metric values without
  applying it
- `action audit/history` — show recent action executions, filterable
- `action revert <id>` — request reversal of a previous action (operator handles)
- `plugin list` — show installed action plugins and their capabilities

The CLI is a thin client. Logic lives in the operator. If you find yourself
implementing business logic in the CLI, stop — it belongs in `internal/`.

### 1.3 Observability assets (`observability/`)

Not a runtime component. Static YAML and JSON files that ship in the Helm chart:

- `grafana-dashboard.json` — visualizes the operator's own metrics
- `prometheus-rules.yaml` — example `PrometheusRule` resources users can adapt

## 2. Data flow

The reconciliation loop for a single `ReactivePolicy`:

```
[1] ReactivePolicy CR exists in cluster
    └─> Controller's Reconcile() called
        │
[2]     ├─> Fetch the policy
        ├─> Check cooldown: is lastTriggeredAt + cooldown > now?
        │   └─> Yes: requeue at (lastTriggeredAt + cooldown), exit
        ├─> Check rate limit: triggerCount in last hour < maxTriggersPerHour?
        │   └─> No: requeue at next hour boundary, exit
        │
[3]     ├─> Query the metric source (Prometheus)
        │   └─> Get current metric value
        │
[4]     ├─> Evaluate threshold
        │   └─> Update status.currentMetricValue, status.lastEvaluatedAt
        │   └─> If threshold not crossed: requeue at pollInterval, exit
        │
[5]     ├─> Threshold crossed: enter action pipeline
        │   ├─> Find the first action with requiresApproval (the gate)
        │   ├─> For each action up to the gate (or all, if there is none):
        │   │   ├─> Look up plugin in registry
        │   │   ├─> Validate params
        │   │   ├─> Execute action with target context
        │   │   ├─> Record result (success/failure)
        │   │   └─> On failure: apply failurePolicy (continue/stop/rollback)
        │   │
[6]     ├─> Write ActionAudit record(s)
        │   └─> Gated: written in Pending phase, carrying the held plugins,
        │       the resolved targets, and an expiry. Pipeline stops here.
        ├─> Update status.triggerCount++
        │   ├─> Gated: status.pendingGateRef, state=AwaitingApproval,
        │   │   and lastTriggeredAt is NOT set — cooldown starts at the
        │   │   decision, not here
        │   └─> Ungated: status.lastTriggeredAt
        ├─> Emit Kubernetes Event
        ├─> Increment internal Prometheus counters
        │
[7]     └─> Requeue at pollInterval
```

Step 6 is critical for the audit trail. Even if action execution fails halfway,
the controller MUST write what happened before the reconcile returns.

A gated pipeline finishes in the **ActionAudit** reconciler rather than here.
When a decision lands (or the gate expires), it runs or skips the held actions,
appends their outcomes to the same record, and starts the policy's cooldown from
that moment. While `status.pendingGateRef` is set the policy reconciler exits at
step 2 without evaluating, so a metric that stays bad cannot queue a second gate
behind the first. See ADR-011.

## 3. Plugin architecture

Plugins are Go packages that implement the `action.Action` interface (defined in
`internal/action/interface.go`, fully documented in `docs/PLUGIN_INTERFACE.md`).

### 3.1 Registration model — Static, compile-time

```go
// cmd/operator/main.go
import (
    _ "github.com/Vedooo/reactive-policy/plugins/notify-slack"
    _ "github.com/Vedooo/reactive-policy/plugins/k8s-annotate"
    _ "github.com/Vedooo/reactive-policy/plugins/argocd-suspend"
)
```

Each plugin's `init()` function calls `action.Register(myPlugin)`. The registry
is a process-global map keyed by plugin name.

**Why not dynamic loading (gRPC/WASM)?** See ADR-001 in DECISIONS.md. Short
version: static loading is simpler, faster, and v0.1 doesn't need the flexibility.

### 3.2 Plugin lifecycle

```
[boot]     init() registers plugin
[validate] ValidatingWebhook calls plugin.Validate() on each action in a policy
[execute]  Controller calls plugin.Execute() when threshold crossed
[reverse]  Optional: action.revert CLI command triggers plugin.Reverse()
```

Plugins are stateless. Any state (e.g., "last time I notified") lives in the
ActionAudit records, not in the plugin.

### 3.3 RBAC aggregation

Each plugin declares its required Kubernetes permissions via
`RequiredPermissions() []rbacv1.PolicyRule`. The Helm chart aggregates these into
the operator's ClusterRole at install time. This means users can see exactly what
permissions they're granting by listing the installed plugins.

## 4. The operator pod

A single Deployment with `replicas: 1`. Leader election is enabled so multiple
replicas can be deployed for HA without conflicts, but only one is active.

Resource requests (defaults):

- CPU: 100m request, 500m limit
- Memory: 128Mi request, 512Mi limit

These are starting points. Heavy policies (many resources, fast poll intervals)
may need more.

Security context:

- `runAsNonRoot: true`
- `readOnlyRootFilesystem: true`
- `seccompProfile: { type: RuntimeDefault }`
- Drops all capabilities

## 5. Webhooks

### 5.1 ValidatingWebhook

Called on CREATE and UPDATE of `ReactivePolicy`. It:

- Validates structural correctness (handled by CRD schema)
- Calls `plugin.Validate(params)` on each action in `spec.actions`
- Rejects policies referencing unknown plugins
- Rejects policies with cooldown < 30s (sanity floor)
- Rejects policies with `pollInterval < 10s`

### 5.2 MutatingWebhook — approval decisions

Registered on `UPDATE` of `ActionAudit` at
`/mutate-reactive-policy-io-v1alpha1-actionaudit`.

It exists because Kubernetes does not persist *who* set a field. If the
approver's identity were an ordinary field, anyone able to record a decision
could also record whose decision it was. The handler reads
`AdmissionRequest.UserInfo` instead and stamps `spec.gate.decidedBy` and
`decidedAt`, overwriting whatever the client sent. Clients choose the verdict;
the API server decides whose name goes next to it.

It also enforces the gate's shape: only the operator may open or remove a gate,
a decision is write-once, and a gate that has expired or already reached a
terminal phase admits no verdict.

Both webhooks are optional (`ENABLE_WEBHOOKS`, `webhook.enabled` in the chart).
With them off, gates still hold pipelines and still expire closed, but the
identity and write-once guarantees are gone — the operator logs a warning at
startup.

## 6. State and persistence

The operator is stateless. All state lives in the cluster as Kubernetes objects:

- `ReactivePolicy` CR — policy definition + last-known status
- `ActionAudit` CR — history of executed actions (see ADR-003)
- Kubernetes Events — short-term operational visibility
- Annotations on target resources — for actions that annotate

`ActionAudit` is written once and never touched again, with one exception: a
gated record is completed by the operator when its approval resolves, appending
the outcomes of the actions it held (ADR-011). That is why the admission webhook
restricts what a client may change on a record rather than freezing it outright.

No external storage is required — the CRD is always the source of truth. Since
v0.3 an optional, off-by-default sink can forward outcomes to Postgres for
longer retention and analytics (`--audit-sink=postgres`); sink errors are logged
and never fail a reconcile. Without it, longer-than-retention history is
exported from the K8s API into a SIEM like any other object.

## 7. Concurrency and races

Each `ReactivePolicy` is reconciled by a single goroutine at a time (standard
controller-runtime behavior). Multiple policies reconcile concurrently with a
default worker count of 4.

Race conditions to watch for:

- **Two policies targeting the same resource.** Allowed by design. Actions run in
  the order their parent policies were last triggered. The audit log shows both.
- **Policy edit during reconciliation.** Handled by resource version conflict
  detection. The reconcile retries on conflict.
- **Operator restart mid-action.** Action execution is best-effort but NOT
  transactional. If the operator dies after executing action 2 of 3, on restart
  it sees the audit records and the cooldown timer, treats the policy as
  triggered, and does NOT retry the remaining actions. This is intentional:
  retrying potentially-destructive actions on restart is more dangerous than
  occasionally completing partially.

## 8. Logging and observability

The operator logs structured JSON via zap (controller-runtime default). Log
levels:

- `Info` — one line per reconcile, one line per action execution
- `V(1)` — per-step debug (metric query, threshold eval, etc.)
- `Error` — action failures, API errors

Own metrics exposed at `:8080/metrics`:

```
reactive_policy_evaluations_total{policy, namespace, result}
reactive_policy_actions_total{plugin, target_kind, result}
reactive_policy_action_duration_seconds{plugin}
reactive_policy_active_policies{namespace}
reactive_policy_triggered_policies_total{namespace}
reactive_policy_rate_limited_total{namespace}
reactive_policy_prometheus_query_errors_total
reactive_policy_plugin_validation_errors_total{plugin}
reactive_policy_approval_gates_opened_total{namespace}
reactive_policy_approval_decisions_total{namespace, outcome}
reactive_policy_approval_wait_seconds{outcome}
reactive_policy_approval_gates_pending{namespace}
```

The Grafana dashboard in `observability/` visualizes these, and
`prometheus-rules.yaml` ships example alerts. The one worth wiring to a pager is
`reactive_policy_approval_gates_pending`: a gate nobody answers is a pipeline
stalled mid-incident, and it expires closed rather than acting.

## 9. Testing strategy

- **Unit tests** (`*_test.go` in each package): pure functions, mocked
  dependencies via interfaces.
- **Controller tests** (`internal/controller/*_test.go`): use envtest (a
  controller-runtime test framework that spins up etcd + kube-apiserver
  locally). Each test creates a `ReactivePolicy`, asserts the controller does
  the right thing.
- **Plugin tests**: each plugin has table-driven tests for `Validate`,
  `Execute`, `Reverse`.
- **E2E tests** (`test/e2e/`): kind-based, run against a real cluster with a
  real Prometheus + ArgoCD. Manual for v0.1, automated in v0.2.

## 10. Build and release

```
make build         # builds both operator and cli binaries
make test          # runs unit + controller tests
make e2e           # runs e2e (requires kind + helm)
make docker-build  # builds container image
make helm-package  # packages the Helm chart
make release       # tags + pushes (CI only)
```

CI (GitHub Actions):

- On push to any branch: lint + test
- On push to main: lint + test + docker-build (no push)
- On tag `v*`: full release — binaries, container images, Helm chart to GHCR
