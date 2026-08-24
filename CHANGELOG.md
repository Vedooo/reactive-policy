# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.1] - 2026-08-25

### Added

- Grafana dashboard gains an approval-gate row (pending gates, outcomes by
  namespace, wait-time p50/p95, gates opened), and `prometheus-rules.yaml` gains
  `ApprovalGatePending` and `ApprovalGateExpired`. The rules are now validated by
  `promtool` in CI, which the docs already claimed but nothing enforced.

### Fixed

- **The chart's `PrometheusRule` had its own hand-copied duplicate of the alert
  rules** and had fallen behind — the two approval alerts were missing from it,
  and every earlier rule existed in two places. The template now embeds
  `observability/prometheus-rules.yaml` verbatim via `.Files.Get`, so the rules
  are written once; `make sync-alerts` (wired into `make manifests`) keeps the
  packaged copy in step and `make verify-manifests` fails CI if it drifts. As a
  side effect Prometheus' own `{{ $labels.x }}` templating no longer needs
  escaping in the chart.
- Documentation that had drifted behind the last two releases: `ARCHITECTURE.md`
  still described the mutating webhook as a future v0.2 idea and stated the
  operator uses no external storage, which the v0.3 Postgres sink had already
  made untrue. Its data-flow, webhook, state and metrics sections now match the
  code, including where a gated pipeline resumes.
- `docs/index.md` and the stack chart README now cover approval gates and how to
  turn the webhooks on through the umbrella.

### Changed

- Both charts move to `0.4.1`.

## [0.4.0] - 2026-08-24

### Added

- **Human approval gates.** An action can set `requiresApproval: true`, which
  holds the pipeline immediately before it runs. Actions ahead of the gate still
  fire at trigger time, so a policy can notify and annotate during the incident
  and hold only its destructive step. The operator writes the `ActionAudit`
  *before* the gated action rather than after, so the approval token and the
  audit trail are one object: the record carries the metric value, what already
  ran, which plugins are held, and the exact resources they would touch. See
  ADR-011.
  - `rp action pending` lists gates waiting on a human with the evidence to
    judge them; `rp action approve` / `rp action deny` record a verdict, with an
    optional `--reason`.
  - `spec.approvalTimeout` (default `30m`, min `1m`, max `24h`) bounds the wait.
    Expiry is fail-closed — an unanswered gate never runs what it held.
  - The approver's identity is stamped by a new admission webhook from the
    authenticated request rather than read from the object, so a client cannot
    name its own approver. Decisions are write-once, and a lapsed or already
    closed gate admits no verdict. With `ENABLE_WEBHOOKS=false` the gate still
    holds the pipeline but those guarantees do not apply; the operator logs a
    warning at startup.
  - While a gate is open the policy does not trigger again — the metric is still
    bad, so a second identical request would otherwise queue behind the first —
    and the cooldown starts from the decision rather than the trigger, so time
    spent waiting on a human does not consume the quiet period that is meant to
    follow the action.
  - The resumed pipeline is confined to the targets resolved at trigger time. A
    resource that starts matching the selector while the gate is open is not
    touched, because the approver never saw it.
  - New policy state `AwaitingApproval` and status field `pendingGateRef`; new
    `ActionAudit` fields `spec.gate`, `status.approvalPhase`, and
    `status.resumedAt`, plus an `Approval` print column.
  - Four new metrics: `reactive_policy_approval_gates_opened_total`,
    `reactive_policy_approval_decisions_total{outcome}`,
    `reactive_policy_approval_wait_seconds`, and
    `reactive_policy_approval_gates_pending`.
- `Executor.RunRange` executes a slice of a pipeline, which is how a gate splits
  one. Rollback stays inside the half that ran: actions released by an approval
  cannot reverse actions audited as complete before the approver looked.
- **The chart can now actually install the webhooks.** `webhook.enabled=true`
  previously only flipped `ENABLE_WEBHOOKS` on the operator — no webhook
  configurations were ever created, so nothing was intercepted. The chart now
  renders a webhook `Service`, both `ValidatingWebhookConfiguration` and
  `MutatingWebhookConfiguration`, and mounts a serving certificate. Three cert
  paths are supported: a self-signed cert-manager `Issuer` and `Certificate`
  created by the chart (default), your own issuer via
  `webhook.certManager.issuerRef`, or a bring-your-own Secret via
  `webhook.existingSecret` + `webhook.caBundle`. `webhook.failurePolicy`
  defaults to `Fail`. Still off by default, so a plain install needs no
  cert-manager.

### Changed

- The validating webhook rejects a pipeline with more than one gated action — a
  single trigger cannot need two separate decisions — and enforces the
  `approvalTimeout` bounds.
- Both charts move to `0.4.0`.

### Fixed

- The Helm chart's bundled CRDs were a hand-copied snapshot of the generated
  ones and had drifted. Helm installs `crds/` verbatim, so any API field added
  since the last manual copy was silently pruned on a chart install. `make
  manifests` now refreshes the chart copies as part of generation, and CI fails
  if generated output is not committed (`make verify-manifests`), so the two
  cannot diverge again.

## [0.3.1] - 2026-08-21

### Changed

- Bump grouped Go dependencies (Dependabot): Kubernetes API/client modules
  0.36.2 → 0.36.3, `prometheus/client_golang` 1.23.2 → 1.24.1,
  `prometheus/common` 0.68.1 → 0.70.1, `ginkgo/v2` 2.31.0 → 2.32.0, `gomega`
  1.42.0 → 1.42.1, `go-logr/logr` 1.4.3 → 1.4.4, plus several `golang.org/x/*`
  patch bumps. This release exists to republish the operator image on the
  refreshed dependency set; there are no functional changes.
- Bump grouped GitHub Actions (Dependabot) across the CI, release, and docs
  workflows: `actions/checkout` v6 → v7, `actions/setup-go` v6 → v7,
  `actions/setup-python` v6 → v7, `DavidAnson/markdownlint-cli2-action`
  v23 → v24.
- `docs/ROADMAP.md` now records v0.3 under "Shipped since v0.1.0" and lists a
  human approval gate for high-blast-radius actions as the next item.

## [0.3.0] - 2026-06-21

### Fixed

- Operator now boots gracefully when `--audit-sink=postgres` is set but the
  DSN env var is empty: it logs a warning and falls back to the no-op sink
  rather than failing startup. Combined with `optional: true` on the
  `secretKeyRef` in the deployment template, this breaks the umbrella's
  circular install dependency (operator pod depended on the CNPG-generated
  Secret, which depended on the Cluster CR, which is a post-install hook).
  Once the Secret materialises, restart the operator to pick up the DSN
  (Stack NOTES now print the exact `kubectl rollout restart` command).
- Umbrella chart `Cluster` CR (audit DB) is now applied as a
  `post-install,post-upgrade` hook with `helm.sh/resource-policy: keep`,
  so it lands after CNPG's CRDs are registered. Without the hook, a fresh
  `helm install` of the stack with `audit.enabled=true` failed with
  `resource mapping not found … kind "Cluster" in version
  "postgresql.cnpg.io/v1"`. The Cluster now persists past
  `helm uninstall`; clean it up explicitly with `kubectl delete cluster`.
- Stack `README.md` documents the matching toggles when
  `kube-prometheus-stack.enabled=false` — `ServiceMonitor` /
  `PrometheusRule` must also be disabled to avoid "resource mapping not
  found" for `monitoring.coreos.com/v1` resources.

### Added

- `reactive-policy-stack` umbrella gains an optional CloudNativePG subchart
  and an `audit.*` block (`audit.enabled=true`, `cloudnative-pg.enabled=true`,
  and `reactive-policy.audit.sink=postgres` together) installs a CNPG `Cluster`
  (`rp-audit`, database `audit`, owner `rp`) and wires the operator to it via
  the auto-generated `rp-audit-app` secret. Default is OFF — the lean
  operator install is unaffected.
- Operator chart gains `audit.sink` / `audit.queueSize` /
  `audit.postgres.dsnEnv` / `audit.postgres.dsnSecret.{name,key}` so the
  Postgres sink (#18) can be used standalone, pointed at any existing
  Postgres via an external Secret.
- `release.yaml` chart job now pulls the `cloudnative-pg` repo when packaging
  the umbrella, so the published OCI artifact self-bundles the CNPG subchart.
- Installation docs section "DB-backed audit history (optional)" covers both
  bundled and bring-your-own configurations.
- Postgres audit sink (`internal/audit/sink/postgres`) built on `pgxpool`,
  with an async buffered queue, a single drain worker, embedded idempotent
  schema (`action_executions` + `revert_outcomes`, both with
  `UNIQUE(audit_uid, action_index)` for retry-safe `ON CONFLICT DO NOTHING`
  inserts). Opt-in via flags: `--audit-sink=postgres`, configurable DSN via
  env (`--audit-postgres-dsn-env`, default `RP_AUDIT_POSTGRES_DSN`),
  configurable queue size. Default remains the no-op sink.
- Pluggable audit sink interface (`internal/audit/sink`) with a default no-op
  implementation, wired into both the `ReactivePolicy` and `ActionAudit`
  reconcilers. The operator forwards trigger and revert outcomes to the sink
  in addition to the `ActionAudit` CRD, which remains source of truth. This is
  the foundation for the upcoming DB-backed history (Postgres sink + CNPG
  umbrella subchart).
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

[Unreleased]: https://github.com/Vedooo/reactive-policy/compare/v0.4.1...HEAD
[0.4.1]: https://github.com/Vedooo/reactive-policy/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/Vedooo/reactive-policy/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/Vedooo/reactive-policy/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Vedooo/reactive-policy/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.2.1
[0.2.0]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.2.0
[0.1.0]: https://github.com/Vedooo/reactive-policy/releases/tag/v0.1.0
