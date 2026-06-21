# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `retag-image` workflow (`workflow_dispatch`) to backfill the v-stripped image
  tag for releases cut before v0.2.1, so the chart's default `image.tag`
  resolves and Artifact Hub's vulnerability scanner can find the image. Used to
  retag `v0.1.0` and `v0.2.0` to `0.1.0` and `0.2.0` without rebuilding.

### Changed

- Bump grouped Go dependencies (Dependabot): `ginkgo/v2` 2.29 → 2.31,
  `gomega` 1.40 → 1.42, Kubernetes API/client modules 0.36.1 → 0.36.2,
  `prometheus/common` 0.67.5 → 0.68.1, `golang-jwt/v5` 5.3.0 → 5.3.1, plus
  several `golang.org/x/*` and `go.yaml.in/yaml/v2` patch bumps.

## [0.2.1] - 2026-05-27

### Fixed

- Publish the container image under the version tag without the leading `v`
  (e.g. `0.2.1`), matching the Helm chart's `appVersion` so a chart install
  pulls an image tag that exists. The v0.2.0 image was published only as
  `v0.2.0`, which left the chart's default image reference dangling.

## [0.2.0] - 2026-05-27

### Added

- **`network.isolate` plugin** — quarantines a matched workload's pods behind a
  restrictive `NetworkPolicy` (ingress/egress, optional DNS allowance).
  Reversible: revert deletes the policy it created.
- **`mesh.shift` plugin** — drains traffic from a backend by setting its weight
  on a Gateway API `HTTPRoute` (works with Istio, Linkerd, and any Gateway API
  mesh). Reversible: revert restores the previous weights.
- **`reactive-policy-stack` umbrella chart** — a distribution that installs the
  operator plus an optional, bundled kube-prometheus-stack (set
  `kube-prometheus-stack.enabled=false` to bring your own Prometheus).

### Changed

- Upgraded to the current Kubernetes ecosystem: Kubernetes libraries `v0.36`,
  controller-runtime `v0.24`, Go `1.26`, and refreshed the build and CI tooling
  (controller-tools, envtest, golangci-lint v2).

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

[Unreleased]: https://github.com/Vedooo/reactive-policy/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.2.1
[0.2.0]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.2.0
[0.1.0]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.1.0
