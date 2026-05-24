# reactive-policy

Turn sustained Prometheus metric conditions into **deterministic, auditable,
reversible** Kubernetes & GitOps actions.

This chart installs the reactive-policy operator, its RBAC, and the
`ReactivePolicy` / `ActionAudit` CRDs.

## Install

```bash
helm install reactive-policy \
  oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace
```

Pin a version with `--version <x.y.z>`. List versions with
`helm show chart oci://ghcr.io/vedooo/charts/reactive-policy`.

## What gets installed

- The operator (one active replica via leader election)
- `ReactivePolicy` and `ActionAudit` CRDs
- A ClusterRole/RBAC covering the built-in plugins
- Optional: metrics service, `ServiceMonitor`, `PrometheusRule`, validating webhook

## A first policy

```yaml
apiVersion: reactive-policy.io/v1alpha1
kind: ReactivePolicy
metadata:
  name: api-error-rate-guard
spec:
  target:
    selector: { matchLabels: { app: api-service } }
    kinds: [{ apiVersion: apps/v1, kind: Deployment }]
  observe:
    source: prometheus
    endpoint: http://prometheus.monitoring:9090
    query: |
      sum(rate(http_requests_total{status=~"5.."}[2m]))
      / sum(rate(http_requests_total[2m]))
    threshold: "0.05"
    operator: GreaterThan
    duration: 2m
  actions:
    - plugin: k8s.annotate
      params: { key: "reactive-policy.io/incident", value: "5xx {{ .MetricValue }}" }
  cooldown: 10m
```

When the 5xx rate stays above 5% for two minutes, the operator annotates the
matched Deployments — recorded as an `ActionAudit`, reversible with
`rp action revert`.

## Configuration

Common values (the full, documented list is in
[`values.yaml`](https://github.com/Vedooo/reactive-policy/blob/main/charts/reactive-policy/values.yaml)):

| Key | Default | Description |
|---|---|---|
| `replicaCount` | `1` | Operator replicas (leader-elected; one active) |
| `image.repository` / `image.tag` | ghcr image / chart appVersion | Operator image |
| `metrics.enabled` | `true` | Expose the operator's Prometheus metrics |
| `serviceMonitor.enabled` | `false` | Create a `ServiceMonitor` for the Prometheus Operator |
| `prometheusRule.enabled` | `false` | Ship the example alerting rules |
| `webhook.enabled` | `false` | Enable the validating webhook (requires cert-manager) |
| `leaderElection.enabled` | `true` | Leader election for HA |

## Links

- Documentation: <https://vedooo.github.io/reactive-policy>
- Source & issues: <https://github.com/Vedooo/reactive-policy>
- License: Apache-2.0
