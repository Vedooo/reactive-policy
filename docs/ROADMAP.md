# Roadmap — v0.1

Eight-week plan to ship v0.1. Each week is a self-contained chunk with a clear
"done" definition.

## Operating model

- Budget: 5-8 hours/week.
- Each week ends with a `v0.0.N` tag.
- The final week ships `v0.1.0` as a proper release.
- Branch per piece of work: `feature/<short-description>`. A week may span
  several feature branches/PRs — keep each focused, with atomic commits.
- Take a VM snapshot at the end of each week.

## Definition of done for v0.1.0

- [ ] Operator binary builds for linux/amd64 and linux/arm64
- [ ] CLI binary builds for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
- [ ] ReactivePolicy CRD applies cleanly to k8s 1.28+
- [ ] All 3 built-in plugins pass their tests
- [ ] Helm chart installs the operator + CRD + RBAC in one command
- [ ] Grafana dashboard JSON renders correctly with sample data
- [ ] README walks a new user from zero to "policy triggered" in under 10 minutes
- [ ] One demo video (asciinema or screen recording) embedded in README
- [ ] CHANGELOG documents every change since v0.0.1
- [ ] LICENSE (Apache 2.0) present and referenced

## Weekly breakdown

---

### Week 1 — Scaffolding & CRD types

**Goal:** A working repo with the CRD installable but no logic yet.

**Tasks:**

1. `kubebuilder init` and `kubebuilder create api`
2. Define Go types in `api/v1alpha1/reactivepolicy_types.go` matching `docs/CRD_SPEC.md`
3. Run `make manifests` and commit the generated CRD YAML
4. Set up `.github/workflows/ci.yaml` (lint + test)
5. Add Apache 2.0 LICENSE
6. First draft of README.md
7. Add `golangci-lint` config

**Done when:**

- `kubectl apply -f config/crd/bases/` succeeds on a kind cluster
- `kubectl get reactivepolicies` returns "No resources found"
- A sample policy YAML in `config/samples/` applies cleanly
- CI green on a PR

**Tag:** `v0.0.1`

**Snapshot label:** `week-1-complete`

---

### Week 2 — Reconciler & Prometheus client

**Goal:** Operator polls Prometheus and updates `status` on each policy.

**Tasks:**

1. Implement `internal/prometheus/client.go`
2. Implement `internal/prometheus/evaluator.go` with sliding-window duration tracking
3. Implement the `Reconcile` function:
   - Fetch the policy
   - Query Prometheus at `pollInterval`
   - Update `status.currentMetricValue`, `status.lastEvaluatedAt`, conditions
   - Log threshold-crossed events but DO NOT execute actions yet
4. Add envtest-based controller tests

**Done when:**

- Applying a sample policy populates `status.currentMetricValue`
- Killing Prometheus shows `MetricSourceReachable=False` condition
- Tests pass in CI with envtest

**Tag:** `v0.0.2`
**Snapshot label:** `week-2-complete`

---

### Week 3 — Action interface & plugin registry

**Goal:** The plugin framework exists. No real plugins yet.

**Tasks:**

1. Implement `internal/action/interface.go` per docs/PLUGIN_INTERFACE.md
2. Implement `internal/action/registry.go` with Register/Lookup/All
3. Implement `internal/action/executor.go` with sequential execution + onFailure
4. Wire executor into Reconcile when threshold crossed + cooldown passed
5. Implement nop test plugin in `internal/action/testing/`
6. Implement ValidatingWebhook for plugin existence and params validation

**Done when:**

- Policy with `actions: [{ plugin: nop }]` triggers and test plugin Execute called
- Policy with unknown plugin rejected by webhook
- Cooldown and rate-limit logic prevent re-triggering

**Tag:** `v0.0.3`
**Snapshot label:** `week-3-complete`

---

### Week 4 — `notify.slack` and `k8s.annotate` plugins

**Tasks:**

1. Implement `plugins/notify-slack/plugin.go` per PLUGIN_INTERFACE.md 5.1
2. Implement `plugins/k8s-annotate/plugin.go` per 5.2
3. Wire into `cmd/operator/main.go`
4. Full test coverage (Validate, Execute, Reverse, edge cases)
5. Sample policy using both plugins

**Done when:**

- Sample policy with both plugins triggers: annotation appears, Slack mock receives message
- Reverse removes annotation in rollback case
- 90%+ test coverage on both plugin packages

**Tag:** `v0.0.4`
**Snapshot label:** `week-4-complete`

---

### Week 5 — `argocd.suspend` plugin & RBAC aggregation

**Tasks:**

1. Implement `plugins/argocd-suspend/plugin.go` per 5.3
2. Implement `internal/action/rbac.go` to aggregate permissions
3. Update Helm chart templates to use aggregated RBAC
4. Integration test in test/e2e/ against real ArgoCD in kind

**Done when:**

- All three plugins work end-to-end
- ClusterRole has aggregated permissions
- Reverse correctly restores ArgoCD auto-sync

**Tag:** `v0.0.5`
**Snapshot label:** `week-5-complete`

---

### Week 6 — CLI, cooldown, rate limit polish

**Tasks:**

1. Implement `cmd/cli/main.go` with Cobra
2. Implement `pkg/cli/policy.go` with list/get/describe/dry-run
3. Implement `pkg/cli/action.go` with audit/history/revert
4. Implement `pkg/cli/plugin.go` with list
5. Move rate-limit state to ActionAudit-backed

**Done when:**

- `rp policy list -n production` shows status table
- `rp policy dry-run` prints what would happen without executing
- `rp action audit --since 1h` shows recent actions
- Rate limit survives operator restart

**Tag:** `v0.0.6`
**Snapshot label:** `week-6-complete`

---

### Week 7 — Self-observability

**Tasks:**

1. Metrics in `internal/metrics/metrics.go` per ARCHITECTURE.md section 8
2. Instrument controller, executor, plugins
3. ServiceMonitor template in Helm chart
4. `observability/grafana-dashboard.json` with 6-8 panels
5. `observability/prometheus-rules.yaml` with example alerts

**Done when:**

- `curl localhost:8080/metrics` shows all documented metrics
- Grafana dashboard imports cleanly
- `promtool check rules` passes

**Tag:** `v0.0.7`
**Snapshot label:** `week-7-complete`

---

### Week 8 — Release engineering & documentation

**Tasks:**

1. Finalize `charts/reactive-policy/`
2. Set up `.github/workflows/release.yaml`
3. Write production README
4. Record demo video
5. Final CHANGELOG.md
6. Tag `v0.1.0`

**Done when:**

- Fresh kind cluster + helm install + sample policy triggers notification in < 5min
- Demo video plays in README on GitHub
- All docs linked from README
- v0.1.0 release page complete

**Tag:** `v0.1.0`
**Snapshot label:** `v0.1.0-released` (keep this snapshot forever)

---

## Beyond v0.1.0

Shipped since v0.1.0:

- **v0.2:** Kubernetes currency (k8s 1.36, controller-runtime 0.24), the
  `network.isolate` and `mesh.shift` action plugins, and the
  `reactive-policy-stack` umbrella chart (operator plus an optional, bundled
  kube-prometheus-stack).
- **v0.3:** DB-backed audit history — a pluggable audit sink with a Postgres
  implementation (async queue, embedded idempotent schema), wired into the
  operator chart and backed by an optional CloudNativePG subchart in the
  umbrella. Off by default; the `ActionAudit` CRD remains source of truth.

On the roadmap, in rough order (not pre-committed):

- A human approval gate for high-blast-radius actions: an opt-in, per-action
  pre-execution hold recorded on the `ActionAudit` itself.
- A web UI over the audit history.
- More action plugins and metric sources (Flux, Loki/Tempo, ...).
- Optional AI/agent-assisted automation.
- **v1.0:** stable API, CNCF Sandbox application.
