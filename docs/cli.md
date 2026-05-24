# CLI reference (`rp`)

`rp` is a thin, stateless client. Every view reads straight from the Kubernetes
API — it stores nothing. It links the same plugin registry as the operator, so
`plugin list` and `policy dry-run` validate against the real plugins.

## Global flags

| Flag | Description |
|---|---|
| `-n, --namespace` | Namespace to operate in (default `default`). |
| `-A, --all-namespaces` | List across all namespaces. |
| `-o, --output` | Output format: `table` (default), `json`, or `yaml`. |
| `--kubeconfig` | Path to the kubeconfig (defaults to standard loading rules). |

## `rp policy`

Inspect `ReactivePolicy` resources.

```bash
rp policy list -A                 # status table across namespaces
rp policy get <name> -n <ns>      # one policy
rp policy describe <name> -n <ns> # full config, status, and conditions
rp policy dry-run policy.yaml     # simulate against live metrics, mutate nothing
```

`dry-run` queries the metric source and reports whether the policy *would* cross
its threshold right now, without creating anything or running actions — useful
for validating a policy before applying it.

```console
$ rp policy list -A
NAMESPACE   NAME               STATE      LAST TRIGGERED   COUNT   VALUE   AGE
demo        error-rate-guard   Watching   -                0       0.012   2m
```

## `rp action`

Inspect and revert recorded executions. (Aliases: `actions`, `audit`.)

```bash
rp action audit -n <ns>              # recent triggers, one row each
rp action history <policy> -n <ns>   # per-action history for one policy
rp action revert <audit-name> -n <ns>
```

```console
$ rp action audit -n demo
NAME                     POLICY             TRIGGERED   ACTIONS   OUTCOME     REVERTED
error-rate-guard-2z7jk   error-rate-guard   23s ago     1         Succeeded   false
```

`revert` requests the operator to reverse a recorded trigger's reversible
actions; the operator performs the reversal and marks the audit `REVERTED`.

## `rp plugin`

```bash
rp plugin list   # installed plugins, reversibility, permission count
```

```console
$ rp plugin list
NAME             REVERSIBLE   PERMISSIONS   DESCRIPTION
argocd.suspend   true         1             Suspends auto-sync on a target ArgoCD Application.
k8s.annotate     true         1             Adds or updates an annotation on the target resource.
notify.slack     false        1             Sends a message to a Slack channel via an incoming webhook.
```

## Output formats

Every command supports `-o json` and `-o yaml` for scripting:

```bash
rp policy describe error-rate-guard -n demo -o yaml
rp action audit -n demo -o json | jq '.[].metadata.name'
```
