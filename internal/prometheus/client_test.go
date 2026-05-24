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

package prometheus

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T, status int, body string, authSeen *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authSeen != nil {
			*authSeen = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientQuery(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    float64
		wantErr error
		anyErr  bool
	}{
		{
			name:   "single sample vector",
			status: http.StatusOK,
			body:   `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"x"},"value":[1620000000,"0.012"]}]}}`,
			want:   0.012,
		},
		{
			name:   "scalar",
			status: http.StatusOK,
			body:   `{"status":"success","data":{"resultType":"scalar","result":[1620000000,"42"]}}`,
			want:   42,
		},
		{
			name:    "empty vector",
			status:  http.StatusOK,
			body:    `{"status":"success","data":{"resultType":"vector","result":[]}}`,
			wantErr: ErrNoData,
		},
		{
			name:   "multi-sample vector",
			status: http.StatusOK,
			body:   `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1620000000,"1"]},{"metric":{},"value":[1620000000,"2"]}]}}`,
			anyErr: true,
		},
		{
			name:   "server error",
			status: http.StatusInternalServerError,
			body:   `{"status":"error","errorType":"internal","error":"boom"}`,
			anyErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t, tc.status, tc.body, nil)
			c, err := NewClient(srv.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			got, err := c.Query(context.Background(), "up")
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
			case tc.anyErr:
				if err == nil {
					t.Fatalf("expected an error, got value %v", got)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Errorf("Query = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestClientBearerToken(t *testing.T) {
	var auth string
	srv := newTestServer(t, http.StatusOK,
		`{"status":"success","data":{"resultType":"scalar","result":[1620000000,"1"]}}`, &auth)

	c, err := NewClient(srv.URL, WithBearerToken("s3cr3t"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Query(context.Background(), "up"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if want := "Bearer s3cr3t"; auth != want {
		t.Errorf("Authorization header = %q, want %q", auth, want)
	}
}
