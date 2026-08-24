# ReactivePolicy CRD Specification

This is the canonical specification for the `ReactivePolicy` custom resource in
v0.1. Any change to the schema must update this document first, then the Go
types, then the validation logic.

## 1. Group, version, kind

- **Group:** `reactive-policy.io`
- **Version:** `v1alpha1`
- **Kind:** `ReactivePolicy`
- **Plural:** `reactivepolicies`
- **Short names:** `rp`, `rpolicy`
- **Scope:** Namespaced

## 2. Full example

```yaml
apiVersion: reactive-policy.io/v1alpha1
kind: ReactivePolicy
metadata:
  name: api-error-rate-guard
  namespace: production
spec:
  target:
    selector:
      matchLabels:
        app: api-service
    kinds:
      - apiVersion: argoproj.io/v1alpha1
        kind: Application
    maxResources: 5

  observe:
    source: prometheus
    endpoint: http://prometheus.monitoring:9090
    query: |
      sum(rate(http_requests_total{app="api-service",status=~"5.."}[2m]))
      / sum(rate(http_requests_total{app="api-service"}[2m]))
    threshold: "0.05"
    operator: GreaterThan
    duration: 2m
    pollInterval: 30s

  actions:
    - plugin: argocd.suspend
      params:
        reason: "auto-suspended by reactive-policy"
      onFailure: stop

    - plugin: k8s.annotate
      params:
        key: "reactive-policy.io/last-trigger"
        value: "{{ .Timestamp }} value={{ .MetricValue }}"
      onFailure: continue

    - plugin: notify.slack
      params:
        webhookSecretRef:
          name: slack-webhook
          key: url
        channel: "#sre-alerts"
        severity: warning
      onFailure: continue

  cooldown: 10m
  maxTriggersPerHour: 3
  allowIrreversible: false

  audit:
    retention: 30d

status:
  state: Watching
  lastEvaluatedAt: "2026-05-20T10:32:15Z"
  lastTriggeredAt: "2026-05-20T09:15:42Z"
  triggerCount: 7
  currentMetricValue: "0.012"
  conditions:
    - type: Ready
      status: "True"
      reason: PoliciesEvaluating
      lastTransitionTime: "2026-05-20T08:00:00Z"
```

## 3. `.spec` fields

### 3.1 `target` (required)

Defines which resources the actions will operate on. Plugins receive this as
their `Target` parameter.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target.selector` | `LabelSelector` | yes | Standard K8s label selector. |
| `target.kinds` | `[]GroupVersionKind` | yes | Resource kinds to match. At least 1, max 10. |
| `target.kinds[].apiVersion` | string | yes | Like `apps/v1` or `argoproj.io/v1alpha1`. |
| `target.kinds[].kind` | string | yes | Like `Deployment` or `Application`. |
| `target.maxResources` | int | no | Safety cap. If selector matches more than this, the policy refuses to act. Default: 10. Max: 100. |

### 3.2 `observe` (required)

Defines the metric to watch and the threshold condition.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `observe.source` | string | yes | Only `prometheus` in v0.1. |
| `observe.endpoint` | string | yes | Full URL of the Prometheus HTTP API. |
| `observe.query` | string | yes | PromQL query. Must return an instant vector with 1 sample. |
| `observe.threshold` | string | yes | Quantity-formatted number to compare against. |
| `observe.operator` | enum | yes | One of `GreaterThan`, `GreaterThanOrEqual`, `LessThan`, `LessThanOrEqual`, `Equal`, `NotEqual`. |
| `observe.duration` | Duration | yes | Threshold must be crossed for this long before triggering. Min `30s`, max `24h`. |
| `observe.pollInterval` | Duration | no | How often to query Prometheus. Default `30s`. Min `10s`, max `5m`. |
| `observe.authSecretRef` | SecretKeySelector | no | For bearer-token auth against Prometheus. |

### 3.3 `actions` (required)

An ordered list of plugin invocations. They run sequentially. At least 1 action.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `actions[].plugin` | string | yes | Plugin name. Must match a registered plugin. |
| `actions[].params` | map[string]JSON | no | Plugin-specific parameters. |
| `actions[].onFailure` | enum | no | `continue`, `stop`, or `rollback`. Default `stop`. |
| `actions[].requiresApproval` | bool | no | Hold the pipeline before this action until a human decides. Default `false`. At most one action per pipeline may set it. |

### 3.4 `cooldown` (optional)

After a successful trigger, the policy will not trigger again for at least this
duration. Default `5m`. Min `30s`, max `24h`.

### 3.5 `maxTriggersPerHour` (optional)

Hard limit on how many times this policy can trigger in any rolling 1-hour
window. Default `5`. Min `1`, max `60`. Prevents runaway policies.

### 3.6 `allowIrreversible` (optional)

Boolean. Default `false`. If `false`, the webhook rejects any policy whose
`actions` includes a plugin whose `IsReversible()` returns false.

### 3.7 `approvalTimeout` (optional)

How long a gated pipeline waits for a decision before the gate is denied.
Default `30m`. Min `1m`, max `24h`. Only meaningful when some action sets
`requiresApproval`. Expiry is fail-closed: the held actions never run.

### 3.8 `audit` (optional)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `audit.retention` | Duration | no | How long to keep `ActionAudit` records. Default `30d`. Max `1y`. |

## 4. `.status` fields

Written by the controller. Users should not edit.

| Field | Type | Description |
|-------|------|-------------|
| `status.state` | enum | One of `Watching`, `Triggering`, `AwaitingApproval`, `Cooldown`, `RateLimited`, `Invalid`. |
| `status.lastEvaluatedAt` | Timestamp | Time of most recent metric evaluation. |
| `status.lastTriggeredAt` | Timestamp | Time of most recent successful trigger. |
| `status.triggerCount` | int | Total triggers since policy creation. |
| `status.currentMetricValue` | string | Most recently observed value. |
| `status.pendingGateRef` | string | Name of the `ActionAudit` holding an open approval gate. While set, the policy does not trigger again. |
| `status.conditions` | []Condition | Standard K8s conditions array. |

### 4.1 Standard conditions

| Type | Meaning |
|------|---------|
| `Ready` | Policy is structurally valid and being evaluated. |
| `PluginValidationPassed` | All referenced plugins exist and validated params. |
| `MetricSourceReachable` | Last metric query succeeded. |
| `ThresholdCrossed` | Threshold currently crossed (transient). |
| `RateLimited` | Hit `maxTriggersPerHour` cap. |
| `AwaitingApproval` | A triggered pipeline is holding for a human decision. |

## 5. Validation behavior

The ValidatingWebhook performs:

1. Structural validation (handled by the CRD's OpenAPI schema).
2. Plugin existence: every `actions[].plugin` must be registered.
3. Plugin parameter validation: each plugin's `Validate()` is called.
4. Sanity floors: cooldown >= 30s, pollInterval >= 10s, etc.
5. `allowIrreversible` enforcement: rejects irreversible actions unless opted-in.
6. Approval gates: at most one action per pipeline may set `requiresApproval`,
   and `approvalTimeout` must be between `1m` and `24h`.

A second admission handler covers decisions on `ActionAudit` records: it stamps
the approver's identity from the authenticated request (a client cannot name its
own approver), makes a decision write-once, and refuses a verdict on a gate that
has already lapsed or closed. Without webhooks enabled the gate still holds the
pipeline, but none of those guarantees apply — see ADR-011.

## 6. Print columns (for `kubectl get`)

```
NAME                       STATE       LAST TRIGGERED   COUNT   AGE
api-error-rate-guard       Watching    9h ago           7       3d
db-pool-saturation         Cooldown    2m ago           3       1d
```

## 7. Future fields (NOT in v0.1)

For reference only. Do not implement.

- `multiSource` — query multiple metric sources and combine (v0.4+)
- `expression` — full CEL expression for threshold (v0.2+)
- `sourceRef` — reference a reusable `MetricSource` CR (v0.2+)
