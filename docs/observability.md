# Observability

A tool that acts on production must itself be observable. The operator exposes
its own Prometheus metrics and ships a Grafana dashboard and example alerts.

## Metrics

Enable the metrics endpoint (on by default in the chart):

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --set metrics.enabled=true --set metrics.secure=false
```

The operator registers eight metrics:

| Metric | Type | Meaning |
|---|---|---|
| `reactive_policy_evaluations_total` | counter | Metric evaluations, labeled by result (crossed / within / error). |
| `reactive_policy_triggered_policies_total` | counter | Triggers fired. |
| `reactive_policy_actions_total` | counter | Action executions, labeled by `plugin`, `target_kind`, `result`. |
| `reactive_policy_action_duration_seconds` | histogram | Per-action execution latency. |
| `reactive_policy_rate_limited_total` | counter | Triggers refused by the hourly rate limit. |
| `reactive_policy_prometheus_query_errors_total` | counter | Failed metric-source queries. |
| `reactive_policy_plugin_validation_errors_total` | counter | Plugin parameter validation failures. |
| `reactive_policy_active_policies` | gauge | Policies currently being reconciled, by namespace. |

### Scrape with the Prometheus Operator

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --set serviceMonitor.enabled=true
```

This creates a `ServiceMonitor` targeting the operator's metrics service.

## Grafana dashboard

Import
[`observability/grafana-dashboard.json`](https://github.com/Vedooo/reactive-policy/blob/main/observability/grafana-dashboard.json)
and select your Prometheus data source. Eight panels: evaluations, actions,
action latency, triggers, rate limiting, query errors, validation errors, and
active policies.

## Alerting rules

[`observability/prometheus-rules.yaml`](https://github.com/Vedooo/reactive-policy/blob/main/observability/prometheus-rules.yaml)
contains example alerts (operator down, high action failure rate, rate-limit
hits, query errors, plugin validation failures), validated with
`promtool check rules`. Deploy them as a `PrometheusRule` via the chart:

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --set prometheusRule.enabled=true
```

See [Architecture](ARCHITECTURE.md) for how the metrics are wired into the
controller and executor.

## Approval gate metrics

Gated pipelines add four series. The one to alert on is `..._decisions_total`
with `outcome="expired"`: it means a pipeline asked for a decision, nobody
answered, and the actions were dropped.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `reactive_policy_approval_gates_opened_total` | counter | `namespace` | Pipelines that stopped for a human decision. |
| `reactive_policy_approval_decisions_total` | counter | `namespace`, `outcome` | Gate resolutions: `approved`, `denied`, `expired`. |
| `reactive_policy_approval_wait_seconds` | histogram | `outcome` | How long a gate waited before resolving. |
| `reactive_policy_approval_gates_pending` | gauge | `namespace` | Gates currently waiting. A gate that lingers here is one nobody has looked at. |

A pending gate is an ordinary object in the cluster, so it is visible from
anywhere someone already looks — and because the count is a metric, an approval
nobody has noticed can page through the same Alertmanager that started the
incident:

```yaml
- alert: ReactivePolicyApprovalPending
  expr: reactive_policy_approval_gates_pending > 0
  for: 10m
  annotations:
    summary: "An action pipeline has been waiting on a human for 10 minutes"
```
