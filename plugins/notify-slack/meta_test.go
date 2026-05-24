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
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/Vedooo/reactive-policy/internal/action"
)

func TestMetadata(t *testing.T) {
	var p plugin
	if p.Name() != "notify.slack" {
		t.Errorf("Name() = %q", p.Name())
	}
	if p.Description() == "" {
		t.Error("Description() should not be empty")
	}
	if p.IsReversible() {
		t.Error("notify.slack should not be reversible")
	}
	rules := p.RequiredPermissions()
	if len(rules) != 1 || rules[0].Resources[0] != "secrets" || rules[0].Verbs[0] != "get" {
		t.Errorf("RequiredPermissions() = %+v, want get on secrets", rules)
	}
}

func TestExecuteNoClientFails(t *testing.T) {
	var p plugin
	res, err := p.Execute(context.Background(), action.ExecuteInput{
		Params:       action.Params{"webhookSecretRef": {Raw: raw(`{"name":"s","key":"url"}`)}},
		Namespace:    "default",
		TemplateData: templateData(),
	})
	if err == nil {
		t.Fatal("Execute without a client in context should fail")
	}
	if res.Status != action.StatusFailed {
		t.Errorf("status = %q, want Failed", res.Status)
	}
}

func TestExecuteNon2xxFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-webhook", Namespace: "default"},
		Data:       map[string][]byte{"url": []byte(server.URL)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secret).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Params:       action.Params{"webhookSecretRef": {Raw: raw(`{"name":"slack-webhook","key":"url"}`)}},
		Namespace:    "default",
		TemplateData: templateData(),
	})
	if err == nil {
		t.Fatal("a non-2xx Slack response should fail Execute")
	}
}

func TestExecuteEmptySecretValueFails(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "slack-webhook", Namespace: "default"},
		Data:       map[string][]byte{"url": []byte("")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(secret).Build()
	ctx := action.WithClient(context.Background(), c)

	var p plugin
	_, err := p.Execute(ctx, action.ExecuteInput{
		Params:       action.Params{"webhookSecretRef": {Raw: raw(`{"name":"slack-webhook","key":"url"}`)}},
		Namespace:    "default",
		TemplateData: templateData(),
	})
	if err == nil {
		t.Fatal("an empty webhook URL should fail Execute")
	}
}
