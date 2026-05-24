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

// Package prometheus queries a Prometheus HTTP API and evaluates the results
// against policy thresholds.
package prometheus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ErrNoData is returned when an instant query yields an empty result set.
var ErrNoData = errors.New("prometheus query returned no data")

type Client interface {
	Query(ctx context.Context, query string) (float64, error)
}

// Factory builds a Client for an endpoint; tests substitute a fake.
type Factory func(endpoint string, opts ...Option) (Client, error)

type Option func(*config)

type config struct {
	bearerToken string
	timeout     time.Duration
}

func WithBearerToken(token string) Option {
	return func(c *config) { c.bearerToken = token }
}

func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

type client struct {
	api     promv1.API
	timeout time.Duration
}

func NewClient(endpoint string, opts ...Option) (Client, error) {
	cfg := config{timeout: 30 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}

	apiCfg := promapi.Config{Address: endpoint}
	if cfg.bearerToken != "" {
		apiCfg.RoundTripper = &bearerRoundTripper{token: cfg.bearerToken, next: promapi.DefaultRoundTripper}
	}

	c, err := promapi.NewClient(apiCfg)
	if err != nil {
		return nil, fmt.Errorf("creating prometheus client for %q: %w", endpoint, err)
	}
	return &client{api: promv1.NewAPI(c), timeout: cfg.timeout}, nil
}

func (c *client) Query(ctx context.Context, query string) (float64, error) {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	result, _, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0, fmt.Errorf("querying prometheus: %w", err)
	}

	switch v := result.(type) {
	case model.Vector:
		switch len(v) {
		case 0:
			return 0, ErrNoData
		case 1:
			return float64(v[0].Value), nil
		default:
			return 0, fmt.Errorf("query returned %d samples, want an instant vector with exactly 1", len(v))
		}
	case *model.Scalar:
		return float64(v.Value), nil
	default:
		return 0, fmt.Errorf("unexpected prometheus result type %T, want an instant vector or scalar", result)
	}
}

type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(clone)
}
