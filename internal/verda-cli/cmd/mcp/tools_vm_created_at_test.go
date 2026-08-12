// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// newInstanceServer wires an MCP Server to a stub replaying exact /instances
// bodies. /v1/instances omits created_at (see temp/docs/c1-ondemand-instance.json),
// and this is the agent surface, so a zero timestamp here is what an autonomous
// reaper would act on — against running instances.
func newInstanceServer(t *testing.T, listBody, oneBody string) *Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(listBody))
	})
	mux.HandleFunc("GET /instances/inst-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oneBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return NewServer(client)
}

func TestListVMsOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	body := `[{"id":"inst-1","hostname":"box-a","status":"running"},` +
		`{"id":"inst-2","hostname":"box-b","status":"running","created_at":"2026-08-11T18:51:12Z"}]`
	s := newInstanceServer(t, body, `{}`)

	res, err := s.handleListVMs(context.Background(), callReq("list_vms", nil))
	if err != nil {
		t.Fatalf("handleListVMs: %v", err)
	}
	got := resultText(t, res)

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("MCP list_vms emits a zero timestamp as data:\n%s", got)
	}
	if !strings.Contains(got, "2026-08-11T18:51:12Z") {
		t.Errorf("the dated instance lost its created_at:\n%s", got)
	}
	if strings.Count(got, "created_at") != 1 {
		t.Errorf("created_at count = %d, want exactly 1:\n%s", strings.Count(got, "created_at"), got)
	}
	if !strings.Contains(got, "box-a") || !strings.Contains(got, "box-b") {
		t.Errorf("instances dropped by the view:\n%s", got)
	}
}

func TestDescribeVMOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	s := newInstanceServer(t, `[]`, `{"id":"inst-1","hostname":"box-a","status":"running"}`)

	res, err := s.handleDescribeVM(context.Background(), callReq("describe_vm", map[string]any{"id": "inst-1"}))
	if err != nil {
		t.Fatalf("handleDescribeVM: %v", err)
	}
	got := resultText(t, res)

	if strings.Contains(got, "0001-01-01") {
		t.Errorf("MCP describe_vm emits a zero timestamp as data:\n%s", got)
	}
	if !strings.Contains(got, "box-a") {
		t.Errorf("hostname missing from the result:\n%s", got)
	}
}
