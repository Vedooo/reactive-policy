/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package notifyslack implements the notify.slack action plugin, which posts a
// message to a Slack channel via an incoming webhook.
package notifyslack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Vedooo/reactive-policy/internal/action"
)

const (
	pluginName     = "notify.slack"
	defaultMessage = "[reactive-policy] *{{ .PolicyName }}* in `{{ .Namespace }}` triggered " +
		"(severity: {{ .Severity }}) — metric value `{{ .MetricValue }}` at {{ .Timestamp }}"
)

var validSeverities = map[string]bool{"info": true, "warning": true, "critical": true}

// httpClient is shared and safe for concurrent use across reconcile goroutines.
var httpClient = &http.Client{Timeout: 10 * time.Second}

type plugin struct{}

func init() { action.Register(&plugin{}) }

func (*plugin) Name() string { return pluginName }

func (*plugin) Description() string {
	return "Sends a message to a Slack channel via an incoming webhook."
}

func (*plugin) Validate(raw action.Params) error {
	var p params
	if err := action.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.WebhookSecretRef.Name == "" || p.WebhookSecretRef.Key == "" {
		return fmt.Errorf("%w: webhookSecretRef.name and webhookSecretRef.key are required", action.ErrInvalidParams)
	}
	if p.Severity != "" && !validSeverities[p.Severity] {
		return fmt.Errorf("%w: severity must be one of info, warning, critical", action.ErrInvalidParams)
	}
	if p.Template != "" {
		if err := action.ValidateTemplate(p.Template); err != nil {
			return fmt.Errorf("%w: %w", action.ErrInvalidParams, err)
		}
	}
	return nil
}

func (*plugin) Execute(ctx context.Context, in action.ExecuteInput) (action.Result, error) {
	res := action.Result{PluginName: pluginName, Target: in.Target, Timestamp: time.Now()}

	var p params
	if err := action.Unmarshal(in.Params, &p); err != nil {
		res.Status = action.StatusFailed
		return res, err
	}
	severity := p.Severity
	if severity == "" {
		severity = "warning"
	}

	c := action.ClientFrom(ctx)
	if c == nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: no Kubernetes client available", action.ErrPermanent)
	}

	var secret corev1.Secret
	if err := c.Get(ctx, client.ObjectKey{Namespace: in.Namespace, Name: p.WebhookSecretRef.Name}, &secret); err != nil {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("reading webhook secret %s/%s: %w", in.Namespace, p.WebhookSecretRef.Name, err)
	}
	webhookURL := string(secret.Data[p.WebhookSecretRef.Key])
	if webhookURL == "" {
		res.Status = action.StatusFailed
		return res, fmt.Errorf("%w: secret %q has no value at key %q", action.ErrInvalidParams, p.WebhookSecretRef.Name, p.WebhookSecretRef.Key)
	}

	tmpl := p.Template
	if tmpl == "" {
		tmpl = defaultMessage
	}
	text, err := action.RenderTemplate(tmpl, mergeTemplateData(in.TemplateData, severity))
	if err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	if err := postMessage(ctx, webhookURL, text, p.Channel); err != nil {
		res.Status = action.StatusFailed
		return res, err
	}

	res.Status = action.StatusSucceeded
	res.Message = fmt.Sprintf("notified slack (severity %s)", severity)
	res.Details = map[string]any{"severity": severity, "channel": p.Channel}
	return res, nil
}

// Reverse is a no-op: a sent notification cannot be unsent.
func (*plugin) Reverse(context.Context, action.Result) error {
	return action.ErrNotReversible
}

func (*plugin) IsReversible() bool { return false }

func (*plugin) RequiredPermissions() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}},
	}
}

func mergeTemplateData(base map[string]any, severity string) map[string]any {
	data := make(map[string]any, len(base)+1)
	for k, v := range base {
		data[k] = v
	}
	data["Severity"] = severity
	return data
}

func postMessage(ctx context.Context, webhookURL, text, channel string) error {
	payload := map[string]string{"text": text}
	if channel != "" {
		payload["channel"] = channel
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding slack payload: %w", err)
	}

	// #nosec G107 -- the webhook URL is read from a user-configured Secret by design.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to slack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: slack returned status %d", action.ErrPermanent, resp.StatusCode)
	}
	return nil
}
