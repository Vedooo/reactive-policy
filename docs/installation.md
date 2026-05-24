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

### Optional: validating webhook

The webhook rejects invalid policies at admission (bad bounds, irreversible
actions without `allowIrreversible`, unknown plugin params). It is **off by
default** so the chart installs on a fresh cluster with no dependencies. To
enable it you need [cert-manager](https://cert-manager.io/):

```bash
helm upgrade --install reactive-policy oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace \
  --set webhook.enabled=true
```

With the webhook off, invalid policies are still caught — at reconcile time,
surfaced on the policy's status conditions rather than rejected at admission.

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
