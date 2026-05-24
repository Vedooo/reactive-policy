# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-05-24

First public release. A Kubernetes operator and CLI that turn sustained
Prometheus metric conditions into deterministic, auditable action pipelines.

### Added

- **Operator** — watches `ReactivePolicy` resources, polls Prometheus, evaluates
  thresholds over a sustained duration, and runs an ordered action pipeline with
  per-action failure policies (continue/stop/rollback), cooldown, and a rolling
  hourly rate limit.
- **Target resolution** — resolves a policy's target selector × kinds into
  concrete cluster resources at trigger time and runs the pipeline against each
  match, enforcing the `maxResources` safety cap.
- **`ReactivePolicy` CRD** — metric source, threshold/operator/duration, target
  selector, action pipeline, cooldown, `maxTriggersPerHour`, `allowIrreversible`,
  and audit retention.
- **`ActionAudit` CRD** — one queryable record per trigger with per-action
  outcomes; the source of truth for restart-safe rate limiting.
- **Built-in plugins** — `notify.slack`, `k8s.annotate`, `argocd.suspend`.
- **`rp` CLI** — `policy list/get/describe/dry-run`, `action audit/history/revert`,
  `plugin list`, with table/json/yaml output.
- **Validating webhook** — rejects invalid policies at admission, including the
  reversibility opt-in and plugin parameter validation.
- **Self-observability** — eight Prometheus metrics, a Grafana dashboard, and
  example alerting rules under `observability/`.
- **Helm chart** — `charts/reactive-policy/`, installable from
  `oci://ghcr.io/vedooo/charts/reactive-policy`.
- **Release automation** — multi-platform binaries, multi-arch container images,
  and an OCI Helm chart published on tag.

### Release history

The 0.1.0 release was built incrementally; the pre-release tags map to:

- **v0.0.1** — project scaffolding and CRD types.
- **v0.0.2** — reconciler and Prometheus client.
- **v0.0.3** — action framework: interface, registry, executor, webhook,
  cooldown, and rate limiting.
- **v0.0.4** — `notify.slack` and `k8s.annotate` plugins.
- **v0.0.5** — `argocd.suspend` plugin and RBAC aggregation, with a kind-based
  end-to-end test.
- **v0.0.6** — `rp` CLI, the `ActionAudit` CRD, and audit-backed rate limiting
  that survives operator restarts.
- **v0.0.7** — self-observability: metrics, Grafana dashboard, and alert rules.

[0.1.0]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.1.0
