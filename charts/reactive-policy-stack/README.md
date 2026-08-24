# reactive-policy-stack

The **reactive-policy distribution**: the [reactive-policy](https://github.com/Vedooo/reactive-policy)
operator plus an optional, bundled [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack).
Install it on a fresh cluster and you have everything needed to observe
Prometheus metrics and run reactive, auditable, reversible Kubernetes actions —
in one command.

The operator itself stays lean; this umbrella is opt-in. If you only want the
operator, install the [`reactive-policy`](https://artifacthub.io/packages/helm/reactive-policy/reactive-policy)
chart instead.

## Install

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace
```

This installs the operator and a full kube-prometheus-stack (Prometheus,
Alertmanager, Grafana, node-exporter, kube-state-metrics). The operator's own
metrics and alert rules are wired in automatically.

### Bring your own Prometheus

If you already run a monitoring stack, skip the bundled one:

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace \
  --set kube-prometheus-stack.enabled=false
```

Then point each policy's `spec.observe.endpoint` at your existing Prometheus.

## Pointing a policy at the bundled Prometheus

With the bundle installed as release `rps` in namespace `reactive-policy`, the
Prometheus HTTP API is reachable in-cluster at:

```
http://rps-kube-prometheus-stack-prometheus.reactive-policy.svc:9090
```

Use that as the `endpoint` in your `ReactivePolicy`:

```yaml
apiVersion: reactive-policy.io/v1alpha1
kind: ReactivePolicy
metadata:
  name: api-error-rate-guard
spec:
  observe:
    source: prometheus
    endpoint: http://rps-kube-prometheus-stack-prometheus.reactive-policy.svc:9090
    query: sum(rate(http_requests_total{code=~"5.."}[5m]))
    threshold: "1"
    operator: GreaterThan
    duration: 5m
  # ... target + actions
```

## Values

| Key | Default | Description |
|---|---|---|
| `kube-prometheus-stack.enabled` | `true` | Install the bundled monitoring stack. Set `false` to bring your own. |
| `reactive-policy.serviceMonitor.enabled` | `true` | Scrape the operator's own metrics. |
| `reactive-policy.prometheusRule.enabled` | `true` | Install the operator's example alert rules. |
| `reactive-policy.webhook.enabled` | `false` | Enable the admission webhooks (needs cert-manager). |

Any value of the [reactive-policy](https://artifacthub.io/packages/helm/reactive-policy/reactive-policy)
operator chart can be set under the `reactive-policy:` key, and any
kube-prometheus-stack value under `kube-prometheus-stack:`.

## Admission webhooks (optional)

Off by default, and not bundled: unlike Prometheus and Postgres, the stack does
not ship cert-manager as a subchart. Install cert-manager yourself, then turn
the webhooks on through the operator chart's values:

```bash
helm upgrade --install reactive-policy-stack \
  oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace \
  --set reactive-policy.webhook.enabled=true
```

This validates policies at admission and, if you use approval gates, stamps the
approver's identity from the authenticated request. Without it a gate still
holds the pipeline but cannot prove who approved. See the
[operator chart README](https://github.com/Vedooo/reactive-policy/blob/main/charts/reactive-policy/README.md#admission-webhooks)
for the bring-your-own-certificate options.

## Links

- [Documentation](https://vedooo.github.io/reactive-policy)
- [Source](https://github.com/Vedooo/reactive-policy)

## Audit history (optional)

The umbrella can also install a CloudNativePG Cluster as a long-term, queryable
store for every triggered action. The `ActionAudit` CRD remains the source of
truth; the database is best-effort analytics — operator restarts and DB blips
never block reconciliation.

Enable it with three matching flags:

```bash
helm install rp oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --set audit.enabled=true \
  --set cloudnative-pg.enabled=true \
  --set reactive-policy.audit.sink=postgres
```

This installs the CloudNativePG operator subchart, creates a Cluster named
`rp-audit` (overridable via `audit.clusterName`), and configures the
reactive-policy operator to forward action and revert events to it via the
auto-generated `rp-audit-app` Secret's `uri` key.

### Without the bundled Prometheus

If you disable `kube-prometheus-stack`, also disable the operator's
`ServiceMonitor` and `PrometheusRule` (their CRDs come from the bundled
Prometheus Operator):

```bash
helm install rp oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --set kube-prometheus-stack.enabled=false \
  --set reactive-policy.serviceMonitor.enabled=false \
  --set reactive-policy.prometheusRule.enabled=false
```

### Bring your own Postgres

To bring your own Postgres instead, leave `audit.enabled=false` and
`cloudnative-pg.enabled=false`, point the operator at your existing instance:

```bash
helm install rp oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --set reactive-policy.audit.sink=postgres \
  --set reactive-policy.audit.postgres.dsnSecret.name=my-postgres-secret \
  --set reactive-policy.audit.postgres.dsnSecret.key=dsn
```
