# Installation

## Prerequisites

- Kubernetes **1.28+**
- Helm **3.8+** (for OCI registry support)
- A Prometheus-compatible metric source reachable from the cluster
- `kubectl` configured for the target cluster

## Install with Helm (OCI)

The chart is published as an OCI artifact on GitHub Container Registry. It
installs the operator, its RBAC, and the `ReactivePolicy` / `ActionAudit` CRDs.

```bash
helm install reactive-policy \
  oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace
```

Pin a version with `--version <x.y.z>`. List available versions:

```bash
helm show chart oci://ghcr.io/vedooo/charts/reactive-policy
```

!!! note "CRDs"
    The CRDs ship in the chart's `crds/` directory and are installed
    automatically on first install. Helm does **not** upgrade or delete CRDs on
    `helm upgrade`/`uninstall` — manage CRD changes deliberately across major
    upgrades.

## Configuration

Every value is documented inline in
[`values.yaml`](https://github.com/Vedooo/reactive-policy/blob/main/charts/reactive-policy/values.yaml).
The most common knobs:

| Value | Default | Purpose |
|---|---|---|
| `replicaCount` | `1` | Operator replicas (leader-elected; one active). |
| `image.repository` / `image.tag` | ghcr image / chart appVersion | Operator image. |
| `metrics.enabled` | `true` | Expose the operator's own Prometheus metrics. |
| `metrics.secure` | `false` | Serve metrics over HTTPS (with auth) vs plain HTTP. |
| `serviceMonitor.enabled` | `false` | Create a `ServiceMonitor` for the Prometheus Operator. |
| `prometheusRule.enabled` | `false` | Ship the example alerting rules as a `PrometheusRule`. |
| `webhook.enabled` | `false` | Enable the validating admission webhook (needs cert-manager). |
| `leaderElection.enabled` | `true` | Leader election for HA. |
| `resources` | sane defaults | CPU/memory requests and limits. |

### Scrape metrics and ship alerts

If you run the Prometheus Operator:

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace \
  --set serviceMonitor.enabled=true \
  --set prometheusRule.enabled=true
```

See [Observability](observability.md) for the dashboard and alerts.

### Optional: admission webhooks

Two webhooks ship with the operator. The **validating** one rejects invalid
policies at admission (bad bounds, irreversible actions without
`allowIrreversible`, unknown plugin params, more than one approval gate). The
**approval** one stamps the approver's identity onto an approval decision from
the authenticated admission request, makes decisions write-once, and refuses a
verdict on a gate that has expired or already closed.

They are **off by default** so the chart installs on a fresh cluster with no
dependencies. Enabling them needs [cert-manager](https://cert-manager.io/):

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace \
  --set webhook.enabled=true
```

The chart creates a self-signed `Issuer` and `Certificate` and lets cert-manager
inject the CA bundle. To sign with your own issuer, set
`webhook.certManager.issuerRef`; to skip cert-manager entirely, set
`webhook.certManager.enabled=false` and supply `webhook.existingSecret` plus
`webhook.caBundle`. See the
[chart README](https://github.com/Vedooo/reactive-policy/blob/main/charts/reactive-policy/README.md)
for both.

With the webhooks off, invalid policies are still caught — at reconcile time,
surfaced on the policy's status conditions rather than rejected at admission.

**If you use approval gates, enable the webhooks.** Kubernetes does not record
who set a field, so without the approval webhook `decidedBy` is only what the
client wrote. The gate still holds the pipeline and still expires closed, but
the record cannot prove who approved it and a decision can be overwritten. The
operator logs a warning at startup when webhooks are disabled.

## Verify

```bash
kubectl get pods -n reactive-policy
kubectl get crd | grep reactive-policy.io
rp plugin list
```

## Upgrade

```bash
helm upgrade reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --reuse-values
```

Review the [changelog](https://github.com/Vedooo/reactive-policy/blob/main/CHANGELOG.md)
for breaking changes while the API is `v0.x`.

## Uninstall

```bash
helm uninstall reactive-policy --namespace reactive-policy
# CRDs are intentionally left in place; remove them explicitly if desired:
kubectl delete crd reactivepolicies.reactive-policy.io actionaudits.reactive-policy.io
```

## Install the `rp` CLI

Download the binary for your platform from the
[releases page](https://github.com/Vedooo/reactive-policy/releases), or build
from source:

```bash
git clone https://github.com/Vedooo/reactive-policy.git
cd reactive-policy && make build-cli   # produces ./bin/rp
```

Next: the [Quickstart](quickstart.md).

## The reactive-policy-stack umbrella chart

For a fresh cluster, the `reactive-policy-stack` chart installs the operator
together with an optional, bundled kube-prometheus-stack — Prometheus,
Alertmanager, and Grafana — in one command:

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace
```

Already running Prometheus? Bring your own:

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace \
  --set kube-prometheus-stack.enabled=false
```

The operator itself stays lean; the umbrella is opt-in. The chart's NOTES print
the in-cluster Prometheus endpoint to reference from each policy's
`spec.observe.endpoint`.

## DB-backed audit history (optional)

By default the operator records every triggered pipeline in the `ActionAudit`
CRD (kept in etcd). For long-retention analytics on top of that, the umbrella
can also install a CloudNativePG cluster and forward every action and revert
outcome to it. The CRD stays the source of truth; the database is best-effort
analytics.

Enable it with three flags on the stack chart:

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --namespace reactive-policy --create-namespace \
  --set audit.enabled=true \
  --set cloudnative-pg.enabled=true \
  --set reactive-policy.audit.sink=postgres
```

This deploys the CNPG operator, creates a `Cluster` named `rp-audit` with a
database `audit` owned by user `rp`, and wires the operator's
`--audit-sink=postgres` flag to the CNPG-generated `rp-audit-app` Secret (key
`uri`).

To bring your own Postgres instead, leave `audit.enabled` /
`cloudnative-pg.enabled` off and point the operator at your existing instance:

```sh
helm install rps oci://ghcr.io/vedooo/charts/reactive-policy-stack \
  --set reactive-policy.audit.sink=postgres \
  --set reactive-policy.audit.postgres.dsnSecret.name=my-postgres-secret \
  --set reactive-policy.audit.postgres.dsnSecret.key=dsn
```

The schema is applied automatically on operator start. Two tables:

- `action_executions` — one row per action outcome, indexed by
  `(policy_ref, triggered_at DESC)` and `audit_uid`.
- `revert_outcomes` — one row per reversed action.

Both have `UNIQUE (audit_uid, action_index)` so duplicate inserts are no-ops.
