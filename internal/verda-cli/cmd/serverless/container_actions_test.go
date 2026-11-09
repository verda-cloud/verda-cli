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

type actionRecorder struct {
	mu      sync.Mutex
	actions []string
	srv     *httptest.Server
}

func newContainerActionServer(t *testing.T) *actionRecorder {
	t.Helper()
	rec := &actionRecorder{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("POST /container-deployments/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.actions = append(rec.actions, r.PathValue("action"))
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func TestContainerPause_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"pause", "my-api"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pause: %v\nstderr: %s", err, stderr.String())
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.actions) != 1 || rec.actions[0] != "pause" {
		t.Errorf("expected [pause], got %v", rec.actions)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got["name"] != "my-api" || got["action"] != "pause" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestContainerResume_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"resume", "my-api"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("resume: %v\nstderr: %s", err, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got["action"] != "resume" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestContainerRestart_AgentMode_WithYes(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"restart", "my-api", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("restart: %v\nstderr: %s", err, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got["action"] != "restart" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestContainerRestart_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"restart", "my-api"})
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

func TestContainerPurgeQueue_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"purge-queue", "my-api", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("purge-queue: %v\nstderr: %s", err, stderr.String())
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got["action"] != "purge-queue" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestContainerPurgeQueue_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"purge-queue", "my-api"})
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

func TestContainerPause_TableOutput(t *testing.T) {
	t.Parallel()
	rec := newContainerActionServer(t)
	f := newTestFactory(t, rec.srv.URL)
	f.AgentModeOverride = false
	f.OutputFormatOverride = outputTable

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"pause", "my-api"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("pause: %v\nstderr: %s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "Paused") {
		t.Errorf("expected 'Paused' in output; got:\n%s", stdout.String())
	}
}
