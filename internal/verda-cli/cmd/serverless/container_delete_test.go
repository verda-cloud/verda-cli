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

package serverless

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteRecorder struct {
	mu      sync.Mutex
	deleted string
	srv     *httptest.Server
}

func newContainerDeleteServer(t *testing.T) *deleteRecorder {
	t.Helper()
	rec := &deleteRecorder{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("DELETE /container-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.deleted = r.PathValue("name")
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func TestContainerDelete_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newContainerDeleteServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"delete", "my-api", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v\nstderr: %s", err, stderr.String())
	}

	rec.mu.Lock()
	deleted := rec.deleted
	rec.mu.Unlock()
	if deleted != "my-api" {
		t.Errorf("deleted = %q, want my-api", deleted)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got["name"] != "my-api" || got["action"] != "delete" || got["status"] != "completed" {
		t.Errorf("got %v", got)
	}
}

func TestContainerDelete_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	rec := newContainerDeleteServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"delete", "my-api"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q, want CONFIRMATION_REQUIRED", agentErr.Code)
	}
}

func TestContainerDelete_TimeoutMsZero_ReportsAccepted(t *testing.T) {
	t.Parallel()
	rec := newContainerDeleteServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"delete", "my-api", "--yes", "--timeout-ms", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v\nstderr: %s", err, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if got["status"] != "accepted" {
		t.Errorf("status = %q, want accepted (timeout-ms 0)", got["status"])
	}
}

func TestContainerDelete_TableOutput(t *testing.T) {
	t.Parallel()
	rec := newContainerDeleteServer(t)
	f := newTestFactory(t, rec.srv.URL)
	f.AgentModeOverride = false
	f.OutputFormatOverride = outputTable

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"delete", "my-api", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delete: %v\nstderr: %s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "Deleted") {
		t.Errorf("expected 'Deleted' in output; got:\n%s", stdout.String())
	}
}
