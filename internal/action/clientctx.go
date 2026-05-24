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

package action

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type clientKey struct{}

// WithClient returns a context carrying the Kubernetes client that plugins use
// to read and mutate cluster resources. The controller injects it before
// running a pipeline; plugins retrieve it with ClientFrom. This keeps plugins
// stateless (see docs/ARCHITECTURE.md §3.1 and §7) while still giving them
// cluster access in Execute and Reverse.
func WithClient(ctx context.Context, c client.Client) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

// ClientFrom returns the client stored by WithClient, or nil if none is set.
func ClientFrom(ctx context.Context) client.Client {
	c, _ := ctx.Value(clientKey{}).(client.Client)
	return c
}
