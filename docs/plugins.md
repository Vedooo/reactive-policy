# Plugins

Actions are plugins. Each implements one `Action` interface and registers itself
at startup, so adding a plugin never touches the core. Three ship in `v0.x`.

A plugin declares whether it is **reversible** (can `rp action revert` undo it)
and the **RBAC** it needs; the operator aggregates those grants.

## `k8s.annotate`

Adds or updates an annotation on each matched resource. **Reversible** — revert
removes the annotation it added.

```yaml
- plugin: k8s.annotate
  params:
    key: "reactive-policy.io/incident"
    value: "error rate {{ .MetricValue }} at {{ .Timestamp }}"   # Go-template
    overwrite: true        # optional; default false (skip if key already present)
```

Template fields available in `value`: `.MetricValue`, `.Timestamp`, `.PolicyName`,
`.Namespace`, and the target's `.Kind` / `.Name`.

## `argocd.suspend`

Pauses auto-sync on a target ArgoCD `Application` so GitOps stops re-applying a
bad release. **Reversible** — revert resumes auto-sync.

```yaml
- plugin: argocd.suspend
  params:
    reason: "auto-suspended by reactive-policy"
```

Requires the target kind to include `argoproj.io/v1alpha1 Application`.

## `notify.slack`

Sends a formatted message to a Slack channel via an incoming webhook.
**Not reversible** (you can't un-send a message).

```yaml
- plugin: notify.slack
  params:
    webhookSecretRef: { name: slack-webhook, key: url }   # reads the URL from a Secret
    channel: "#sre-alerts"
    severity: warning
```

## Failure handling

Each action sets `onFailure`:

| `onFailure` | Behavior if the action fails |
|---|---|
| `stop` (default) | Halt the pipeline; later actions don't run. |
| `continue` | Record the failure and run the next action. |
| `rollback` | Reverse the already-applied actions in this run, then stop. |

## Writing your own

The plugin interface, registration, RBAC, and reversibility contract are
documented in [Plugin development](PLUGIN_INTERFACE.md).
