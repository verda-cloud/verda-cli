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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newSecretServer(t *testing.T) (*httptest.Server, *secretRecorder) {
	t.Helper()
	rec := &secretRecorder{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rec.mu.Lock()
		resp := rec.listResponse
		rec.mu.Unlock()
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.createBody = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})

	mux.HandleFunc("DELETE /secrets/{name}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.deletedName = r.PathValue("name")
		rec.deleteForce = r.URL.Query().Get("force") == "true"
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

type secretRecorder struct {
	mu           sync.Mutex
	listResponse []verda.Secret
	createBody   []byte
	deletedName  string
	deleteForce  bool
}

func TestSecretList_Table(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)
	rec.listResponse = []verda.Secret{
		{Name: "db-password", SecretType: "opaque", CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)},
		{Name: "api-key", SecretType: "opaque", CreatedAt: time.Date(2026, 4, 1, 8, 30, 0, 0, time.UTC)},
	}

	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret list failed: %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "TYPE") || !strings.Contains(out, "CREATED") {
		t.Errorf("expected table header, got:\n%s", out)
	}
	if !strings.Contains(out, "db-password") {
		t.Errorf("expected db-password in output, got:\n%s", out)
	}
	if !strings.Contains(out, "api-key") {
		t.Errorf("expected api-key in output, got:\n%s", out)
	}
}

func TestSecretList_JSON(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)
	rec.listResponse = []verda.Secret{
		{Name: "db-password", SecretType: "opaque", CreatedAt: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)},
	}

	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret list failed: %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "db-password") {
		t.Errorf("expected db-password in JSON output, got:\n%s", out)
	}
}

func TestSecretList_Empty(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)
	rec.listResponse = []verda.Secret{}

	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret list failed: %v\nstderr: %s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "No secrets found.") {
		t.Errorf("expected 'No secrets found.' message, got:\n%s", stdout.String())
	}
}

func TestSecretCreate_Flags(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)

	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "create", "--name", "my-secret", "--value", "s3cr3t"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret create failed: %v\nstderr: %s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.createBody
	rec.mu.Unlock()
	if body == nil {
		t.Fatal("server did not receive POST /secrets")
	}

	var got struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got.Name != "my-secret" {
		t.Errorf("name: got %q, want my-secret", got.Name)
	}
	if got.Value != "s3cr3t" {
		t.Errorf("value: got %q, want s3cr3t", got.Value)
	}
}

func TestSecretCreate_AgentMode_MissingFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "create"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("expected MISSING_REQUIRED_FLAGS, got code=%q", agentErr.Code)
	}
}

func TestSecretDelete_WithYes(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)

	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "delete", "my-secret", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret delete failed: %v\nstderr: %s", err, stderr.String())
	}

	rec.mu.Lock()
	name := rec.deletedName
	rec.mu.Unlock()
	if name != "my-secret" {
		t.Errorf("expected DELETE for my-secret, got %q", name)
	}

	if !strings.Contains(stdout.String(), "my-secret") {
		t.Errorf("expected output to contain secret name, got:\n%s", stdout.String())
	}
}

func TestSecretDelete_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "delete", "my-secret"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("expected CONFIRMATION_REQUIRED, got code=%q", agentErr.Code)
	}
}

func TestSecretDelete_Force(t *testing.T) {
	t.Parallel()
	srv, rec := newSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "delete", "my-secret", "--yes", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret delete --force failed: %v\nstderr: %s", err, stderr.String())
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !rec.deleteForce {
		t.Error("expected force=true in DELETE request")
	}
	if rec.deletedName != "my-secret" {
		t.Errorf("deleted name: got %q, want my-secret", rec.deletedName)
	}
}

func TestSecretCreate_AgentMode_PartialFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "create", "--name", "my-secret"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code: got %q, want MISSING_REQUIRED_FLAGS", agentErr.Code)
	}
	missing, ok := agentErr.Details["missing"].([]string)
	if !ok {
		t.Fatalf("expected Details[missing] to be []string, got %T", agentErr.Details["missing"])
	}
	if len(missing) != 1 || missing[0] != "--value" {
		t.Errorf("expected missing=[--value], got %v", missing)
	}
}

func TestSecretCreate_JSONOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "create", "--name", "my-secret", "--value", "s3cr3t"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret create failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["name"] != "my-secret" {
		t.Errorf("name: got %q, want my-secret", got["name"])
	}
	if got["action"] != "created" {
		t.Errorf("action: got %q, want created", got["action"])
	}
}

func TestSecretDelete_JSONOutput(t *testing.T) {
	t.Parallel()
	srv, _ := newSecretServer(t)
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"secret", "delete", "my-secret", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("secret delete failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["name"] != "my-secret" {
		t.Errorf("name: got %q, want my-secret", got["name"])
	}
	if got["action"] != "deleted" {
		t.Errorf("action: got %q, want deleted", got["action"])
	}
}
