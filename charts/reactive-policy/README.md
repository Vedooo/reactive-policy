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
- Optional: metrics service, `ServiceMonitor`, `PrometheusRule`, admission webhooks

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
| `webhook.enabled` | `false` | Enable the admission webhooks (see below) |
| `webhook.certManager.enabled` | `true` | Issue the serving cert with cert-manager |
| `webhook.failurePolicy` | `Fail` | Reject writes when the webhook is unreachable |
| `leaderElection.enabled` | `true` | Leader election for HA |

## Admission webhooks

Off by default so the chart installs without cert-manager. Enabling it creates
two configurations:

- **Validation** rejects invalid policies at admission — unknown plugins, bad
  params, irreversible actions without `allowIrreversible`, out-of-range
  timings, and more than one approval gate in a pipeline.
- **Approval** stamps the approver's identity onto a decision from the
  authenticated admission request, makes decisions write-once, and refuses a
  verdict on a gate that has expired or already closed.

That second one matters if you use approval gates. Kubernetes does not record
who set a field, so without the webhook `decidedBy` is only whatever the client
wrote — the gate still holds the pipeline, but the record cannot prove who
approved it. The operator logs a warning at startup when webhooks are off.

With cert-manager in the cluster (the chart creates a self-signed `Issuer` and
`Certificate`):

```bash
helm install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace \
  --set webhook.enabled=true
```

To sign with your own issuer instead:

```bash
  --set webhook.certManager.issuerRef.name=my-ca \
  --set webhook.certManager.issuerRef.kind=ClusterIssuer
```

To skip cert-manager entirely, supply a Secret holding `tls.crt`/`tls.key` plus
the base64-encoded CA that signed it:

```bash
  --set webhook.certManager.enabled=false \
  --set webhook.existingSecret=my-webhook-tls \
  --set webhook.caBundle=$(base64 -w0 < ca.crt)
```

`webhook.failurePolicy` defaults to `Fail`, so an unreachable webhook rejects
the write rather than admitting an unvalidated policy or an unstamped approval.
Set it to `Ignore` only if you would rather lose those guarantees than block
writes while the operator is down.

## Links

- Documentation: <https://vedooo.github.io/reactive-policy>
- Source & issues: <https://github.com/Vedooo/reactive-policy>
- License: Apache-2.0
