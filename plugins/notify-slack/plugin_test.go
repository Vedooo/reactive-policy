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

package notifyslack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func raw(s string) []byte { return []byte(s) }

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		params  action.Params
		wantErr bool
	}{
		"valid": {
			params: action.Params{"webhookSecretRef": {Raw: raw(`{"name":"s","key":"url"}`)}},
		},
		"valid with severity and template": {
			params: action.Params{
				"webhookSecretRef": {Raw: raw(`{"name":"s","key":"url"}`)},
				"severity":         {Raw: raw(`"critical"`)},
				"template":         {Raw: raw(`"{{ .PolicyName }}"`)},
			},
		},
		"missing secret ref": {
			params:  action.Params{"channel": {Raw: raw(`"#x"`)}},
			wantErr: true,
		},
		"bad severity": {
			params: action.Params{
				"webhookSecretRef": {Raw: raw(`{"name":"s","key":"url"}`)},
				"severity":         {Raw: raw(`"loud"`)},
			},
			wantErr: true,
		},
		"bad template": {
			params: action.Params{
				"webhookSecretRef": {Raw: raw(`{"name":"s","key":"url"}`)},
				"template":         {Raw: raw(`"{{ .Nope "`)},
			},
			wantErr: true,
		},
	}

	var p plugin
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := p.Validate(tc.params)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func templateData() map[string]any {
	return map[string]any{
		"PolicyName":  "api-guard",
		"Namespace":   "default",
		"MetricValue": "0.9",
		"Timestamp":   time.Now().Format(time.RFC3339),
		"Target":      action.Target{Namespace: "default"},
	}
}

func TestExecutePostsToWebhook(t *testing.T) {
	var got struct {
		called  int32
		payload map[string]string
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&got.called, 1)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got.payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-webhook", Namespace: "default"},
		Data:       map[string][]byte{"url": []byte(server.URL)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secret).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Params: action.Params{
			"webhookSecretRef": {Raw: raw(`{"name":"slack-webhook","key":"url"}`)},
			"channel":          {Raw: raw(`"#sre-alerts"`)},
			"severity":         {Raw: raw(`"critical"`)},
		},
		PolicyName:   "api-guard",
		Namespace:    "default",
		MetricValue:  "0.9",
		Timestamp:    time.Now(),
		TemplateData: templateData(),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != action.StatusSucceeded {
		t.Fatalf("status = %q, want Succeeded", res.Status)
	}
	if atomic.LoadInt32(&got.called) != 1 {
		t.Fatalf("webhook called %d times, want 1", got.called)
	}
	if got.payload["channel"] != "#sre-alerts" {
		t.Errorf("payload channel = %q, want #sre-alerts", got.payload["channel"])
	}
	if got.payload["text"] == "" {
		t.Error("payload text should not be empty")
	}
}

func TestExecuteMissingSecretFails(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	res, err := p.Execute(ctx, action.ExecuteInput{
		Params:       action.Params{"webhookSecretRef": {Raw: raw(`{"name":"missing","key":"url"}`)}},
		Namespace:    "default",
		TemplateData: templateData(),
	})
	if err == nil {
		t.Fatal("a missing secret should fail Execute")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestNotReversible(t *testing.T) {
	var p plugin
	if p.IsReversible() {
		t.Error("notify.slack should not be reversible")
	}
	if !errors.Is(p.Reverse(context.Background(), action.Result{}), action.ErrNotReversible) {
		t.Error("Reverse should return ErrNotReversible")
	}
}
