# Concepts

A `ReactivePolicy` is a small, declarative contract: *watch this metric; when it
stays bad for long enough, run these actions against these resources — safely,
and keep a record.*

## The reconcile loop

On every poll the operator walks the same gates, in order. A trigger only
happens if all of them pass:

1. **Cooldown** — if the policy triggered recently, stay quiet until the cooldown
   elapses.
2. **Rate limit** — if `maxTriggersPerHour` is already reached in the trailing
   hour, refuse. The count comes from persisted `ActionAudit` records, so it
   holds across operator restarts.
3. **Query & evaluate** — query the metric source and compare the value to the
   threshold with the configured operator.
4. **Sustain** — the condition must stay crossed for the full `duration` (a
   sliding window); a momentary spike never triggers.
5. **Resolve targets** — turn the target selector × kinds into concrete cluster
   resources, enforcing the `maxResources` cap.
6. **Act & audit** — run the action pipeline against each matched resource and
   write one `ActionAudit` for the trigger.

## Anatomy of a policy

```yaml
spec:
  target:                 # which resources actions operate on
    selector: { matchLabels: { app: api-service } }
    kinds: [{ apiVersion: apps/v1, kind: Deployment }]
    maxResources: 5       # refuse to act if the selector matches more than this
  observe:                # what to watch
    source: prometheus
    endpoint: http://prometheus.monitoring:9090
    query: <PromQL returning a single value>
    threshold: "0.05"
    operator: GreaterThan
    duration: 2m          # must stay crossed this long
    pollInterval: 30s
  actions:                # ordered pipeline
    - plugin: k8s.annotate
      params: { ... }
      onFailure: stop     # continue | stop | rollback
  cooldown: 10m
  maxTriggersPerHour: 3
  allowIrreversible: false
```

## Safety rails

These are first-class, not afterthoughts — this tool acts on production, so it's
built to fail safe.

| Mechanism | What it guarantees |
|---|---|
| **Sustained duration** | A momentary spike never triggers. |
| **Cooldown** | A minimum quiet period between triggers; a flapping metric can't thrash. |
| **Hourly rate limit** | `maxTriggersPerHour` caps runaway behavior, counted from persisted audits (restart-safe). |
| **Target cap** | `maxResources` refuses to act on an unexpectedly large match set. |
| **Reversibility** | Irreversible actions require `allowIrreversible: true`; reversible ones undo with `rp action revert`. |
| **Audit trail** | One queryable `ActionAudit` per trigger: when, metric value, targets, outcomes. |

## Targets and fan-out

`spec.target` selects resources by label selector across one or more kinds. The
operator resolves it at trigger time and runs the **full pipeline once per
matched resource**. If a selector matches three Deployments, each gets the
pipeline; all results land in a single `ActionAudit` for that trigger. If a
policy matches nothing, notification-style actions still run once.

## Audit and revert

Every trigger writes an `ActionAudit` — the source of truth for both the rate
limiter and your incident history. Reversible actions can be undone:

```bash
rp action revert <audit-name> -n <namespace>
```

This sets `revertRequested` on the audit; the operator replays each reversible
action's reverse logic (e.g. `k8s.annotate` removes the annotation it added,
`argocd.suspend` resumes auto-sync) and marks the audit reverted.

See [Architecture](ARCHITECTURE.md) for the internals and
[CRD specification](CRD_SPEC.md) for every field.
