# Plugin Interface

This document specifies the contract that every action plugin must implement.
reactive-policy ships five built-in plugins (`notify.slack`, `k8s.annotate`,
`argocd.suspend`, `network.isolate`, `mesh.shift`). New plugins follow the same
pattern.

## 1. The `Action` interface

Located in `internal/action/interface.go`.

```go
package action

import (
    "context"
    "time"

    rbacv1 "k8s.io/api/rbac/v1"
    "k8s.io/apimachinery/pkg/runtime"
)

type Action interface {
    Name() string
    Description() string
    Validate(params Params) error
    Execute(ctx context.Context, in ExecuteInput) (Result, error)
    Reverse(ctx context.Context, prev Result) error
    IsReversible() bool
    RequiredPermissions() []rbacv1.PolicyRule
}
```

## 2. Supporting types

```go
type Params map[string]runtime.RawExtension

type ExecuteInput struct {
    Target       Target
    Params       Params
    PolicyName   string
    Namespace    string
    MetricValue  string
    Timestamp    time.Time
    TemplateData map[string]any
}

type Target struct {
    APIVersion string
    Kind       string
    Name       string
    Namespace  string
}

type Result struct {
    ActionID   string
    PluginName string
    Target     Target
    Timestamp  time.Time
    Status     ResultStatus
    Message    string
    Details    map[string]any
}

type ResultStatus string

const (
    StatusSucceeded ResultStatus = "Succeeded"
    StatusFailed    ResultStatus = "Failed"
    StatusSkipped   ResultStatus = "Skipped"
)
```

## 3. The registry

```go
package action

var registry = map[string]Action{}

func Register(a Action) {
    name := a.Name()
    if _, exists := registry[name]; exists {
        panic("action plugin " + name + " already registered")
    }
    registry[name] = a
}

func Lookup(name string) Action {
    return registry[name]
}

func All() []Action { ... }
```

## 4. How to write a plugin

Each plugin lives in `plugins/<plugin-name>/`. The directory contains:

- `plugin.go` — the `Action` implementation
- `plugin_test.go` — table-driven tests
- `params.go` — typed struct for params (optional but recommended)

### 4.1 Anatomy

```go
package myplugin

import (
    "context"
    "fmt"

    rbacv1 "k8s.io/api/rbac/v1"

    "github.com/Vedooo/reactive-policy/internal/action"
)

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (p *plugin) Name() string        { return "category.verb" }
func (p *plugin) Description() string { return "Does the thing" }

func (p *plugin) Validate(params action.Params) error {
    var typed myParams
    if err := unmarshal(params, &typed); err != nil {
        return fmt.Errorf("invalid params: %w", err)
    }
    if typed.RequiredField == "" {
        return fmt.Errorf("requiredField is required")
    }
    return nil
}

func (p *plugin) Execute(ctx context.Context, in action.ExecuteInput) (action.Result, error) {
    // 1. Unmarshal params
    // 2. Expand templates
    // 3. Do the work
    // 4. Populate Result with enough Details to reverse later
}

func (p *plugin) Reverse(ctx context.Context, prev action.Result) error { ... }
func (p *plugin) IsReversible() bool { return true }

func (p *plugin) RequiredPermissions() []rbacv1.PolicyRule {
    return []rbacv1.PolicyRule{
        {APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create"}},
    }
}
```

### 4.2 Required test cases

- Validate: empty params, missing required field, valid params
- Execute: happy path, target not found, context cancellation, dependency failure
- Reverse: happy path, prev result missing required details
- IsReversible: returns expected value

### 4.3 Plugin naming conventions

- Use `<category>.<verb>` format
- Categories: `notify`, `k8s`, `argocd`, `flux` (v0.2+), `cloud` (v0.3+)
- Verbs: imperative, lowercase
- Examples: `notify.slack`, `k8s.annotate`, `argocd.suspend`

## 5. v0.1 built-in plugins

### 5.1 `notify.slack`

Sends a formatted message to a Slack channel via incoming webhook.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `webhookSecretRef.name` | string | yes | Name of a Secret in the policy's namespace. |
| `webhookSecretRef.key` | string | yes | Key holding the webhook URL. |
| `channel` | string | no | Channel override. |
| `severity` | enum | no | `info`, `warning`, `critical`. Default `warning`. |
| `template` | string | no | Override the default message template. |

**Reversibility:** No. `IsReversible()` returns false.

**Permissions:**

```yaml
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get"]
```

### 5.2 `k8s.annotate`

Adds or updates an annotation on the target resource.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | string | yes | Annotation key. |
| `value` | string | yes | Annotation value. Supports Go templates. |
| `overwrite` | bool | no | Default `true`. |

**Reversibility:** Yes. `Reverse` removes the annotation key.

**Permissions:** Dynamic based on target kind.

### 5.3 `argocd.suspend`

Suspends auto-sync on a target ArgoCD `Application`.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `reason` | string | no | Stored as annotation `reactive-policy.io/suspend-reason`. |

**Reversibility:** Yes. `Reverse` restores stored sync settings.

**Permissions:**

```yaml
- apiGroups: ["argoproj.io"]
  resources: ["applications"]
  verbs: ["get", "patch"]
```

## 6. Template expansion

Plugins access template variables via `in.TemplateData`:

- `{{ .PolicyName }}`
- `{{ .Namespace }}`
- `{{ .MetricValue }}`
- `{{ .Timestamp }}`
- `{{ .Target.Kind }}`, `{{ .Target.Name }}`, `{{ .Target.Namespace }}`

Use Go's `text/template`. Validate templates in `Validate()` so bad templates
fail at admission, not at execute time.

## 7. Error handling

- Wrap errors: `fmt.Errorf("...: %w", err)`
- Sentinel errors: `action.ErrNotReversible`, `action.ErrTargetNotFound`,
  `action.ErrInvalidParams`
- Distinguish transient (retry) from permanent (don't retry):
  return permanent errors wrapped with `action.ErrPermanent`

## 8. Concurrency notes

`Execute` is called from the controller's reconcile goroutine. Multiple
policies can execute the same plugin concurrently. Plugins MUST be safe for
concurrent use — no shared mutable state without a mutex.

## 9. Testing helpers

The `internal/action/testing` package provides:

- `NewFakeRegistry()`
- `NewExecuteInput(t, opts...)`
- `AssertResult(t, got, want)`
- `MockTarget(kind, name)`
