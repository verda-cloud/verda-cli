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

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type envRecordingServer struct {
	mu        sync.Mutex
	envVars   []verda.ContainerEnvVar
	addBody   []byte
	patchBody []byte
	delBody   []byte
	srv       *httptest.Server
}

func newEnvRecordingServer(t *testing.T, vars []verda.ContainerEnvVar) *envRecordingServer {
	t.Helper()
	rec := &envRecordingServer{envVars: vars}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	// Mirrors the API: the single container is named "<deployment>-0", and
	// the env write endpoints key on that, not on the deployment name.
	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name:       name,
			Containers: []verda.DeploymentContainer{{Name: name + "-0", Image: verda.ContainerImage{Image: "nginx:1.27.3"}}},
		})
	})

	// Grouped per container, as the API returns it (one group per container).
	mux.HandleFunc("GET /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		v := rec.envVars
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"container_name": r.PathValue("name") + "-0", "env": v}})
	})

	mux.HandleFunc("POST /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.addBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PATCH /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.patchBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.delBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *envRecordingServer) getAddBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addBody
}

func (r *envRecordingServer) getPatchBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.patchBody
}

func (r *envRecordingServer) getDelBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.delBody
}

func TestEnvList_JSON(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "bar"},
		{Name: "TOKEN", Type: "secret", ValueOrReferenceToSecret: "my-secret"},
	})
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "list", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env list failed: %v\nstderr:\n%s", err, stderr.String())
	}

	var got []struct {
		Name                     string `json:"name"`
		Type                     string `json:"type"`
		ValueOrReferenceToSecret string `json:"value_or_reference_to_secret"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout.String())
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(got))
	}
	if got[0].Name != "FOO" || got[0].Type != "plain" {
		t.Errorf("env[0]: got %+v", got[0])
	}
	if got[1].Name != "TOKEN" || got[1].Type != "secret" {
		t.Errorf("env[1]: got %+v", got[1])
	}
}

func TestEnvList_Empty(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{})
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "list", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env list failed: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "No environment variables") {
		t.Errorf("expected empty message, got: %s", out)
	}
}

func TestEnvSet_AddNew(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=bar"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}

	addBody := rec.getAddBody()
	if addBody == nil {
		t.Fatal("expected POST body for add, got nil")
	}
	var got struct {
		ContainerName string `json:"container_name"`
		Env           []struct {
			Name                     string `json:"name"`
			Type                     string `json:"type"`
			ValueOrReferenceToSecret string `json:"value_or_reference_to_secret"`
		} `json:"env"`
	}
	if err := json.Unmarshal(addBody, &got); err != nil {
		t.Fatalf("unmarshal add body: %v\nbody: %s", err, addBody)
	}
	if got.ContainerName != "my-endpoint-0" {
		t.Errorf("container_name: got %q, want the inner container my-endpoint-0", got.ContainerName)
	}
	if len(got.Env) != 1 || got.Env[0].Name != "FOO" || got.Env[0].ValueOrReferenceToSecret != "bar" {
		t.Errorf("env: got %+v", got.Env)
	}
	if rec.getPatchBody() != nil {
		t.Error("PATCH should not be sent when all vars are new")
	}
}

func TestEnvSet_UpdateExisting(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "old"},
	})
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=new"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}

	if rec.getAddBody() != nil {
		t.Error("POST should not be sent when all vars already exist")
	}
	patchBody := rec.getPatchBody()
	if patchBody == nil {
		t.Fatal("expected PATCH body for update, got nil")
	}
	var got struct {
		ContainerName string `json:"container_name"`
		Env           []struct {
			Name                     string `json:"name"`
			ValueOrReferenceToSecret string `json:"value_or_reference_to_secret"`
		} `json:"env"`
	}
	if err := json.Unmarshal(patchBody, &got); err != nil {
		t.Fatalf("unmarshal patch body: %v\nbody: %s", err, patchBody)
	}
	if len(got.Env) != 1 || got.Env[0].Name != "FOO" || got.Env[0].ValueOrReferenceToSecret != "new" {
		t.Errorf("patch env: got %+v", got.Env)
	}
	if got.ContainerName != "my-endpoint-0" {
		t.Errorf("container_name: got %q, want the inner container my-endpoint-0", got.ContainerName)
	}
}

// A deployment read with no containers has nothing to key the env write on;
// fail before sending rather than let the API return a confusing not_found.
func TestEnvSet_NoContainers(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{Name: "my-endpoint"})
	})
	var wrote bool
	mux.HandleFunc("/container-deployments/{name}/environment-variables", func(w http.ResponseWriter, _ *http.Request) {
		wrote = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=bar"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "has no containers") {
		t.Fatalf("want 'has no containers' error, got %v", err)
	}
	if wrote {
		t.Error("env endpoint must not be called when the container name is unknown")
	}
}

func TestEnvSet_MixedAddUpdate(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "old"},
	})
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=updated", "--env", "NEW=val"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}

	addBody := rec.getAddBody()
	if addBody == nil {
		t.Fatal("expected POST body for new var")
	}
	patchBody := rec.getPatchBody()
	if patchBody == nil {
		t.Fatal("expected PATCH body for existing var")
	}

	var add struct {
		Env []struct{ Name string } `json:"env"`
	}
	if err := json.Unmarshal(addBody, &add); err != nil {
		t.Fatalf("unmarshal add: %v", err)
	}
	if len(add.Env) != 1 || add.Env[0].Name != "NEW" {
		t.Errorf("add should contain NEW, got %+v", add.Env)
	}

	var patch struct {
		Env []struct{ Name string } `json:"env"`
	}
	if err := json.Unmarshal(patchBody, &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	if len(patch.Env) != 1 || patch.Env[0].Name != "FOO" {
		t.Errorf("patch should contain FOO, got %+v", patch.Env)
	}
}

func TestEnvSet_SecretVar(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env-secret", "TOKEN=my-secret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}

	addBody := rec.getAddBody()
	if addBody == nil {
		t.Fatal("expected POST body")
	}
	var got struct {
		Env []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"env"`
	}
	if err := json.Unmarshal(addBody, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Env) != 1 || got.Env[0].Name != "TOKEN" || got.Env[0].Type != "secret" {
		t.Errorf("expected secret env TOKEN, got %+v", got.Env)
	}
}

func TestEnvSet_AgentMode_MissingFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code: got %q, want MISSING_REQUIRED_FLAGS", agentErr.Code)
	}
}

func TestEnvUnset_WithYes(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "HF_HOME", "--deployment", "my-endpoint", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env unset failed: %v\nstderr:\n%s", err, stderr.String())
	}

	delBody := rec.getDelBody()
	if delBody == nil {
		t.Fatal("expected DELETE body, got nil")
	}
	var got struct {
		ContainerName string   `json:"container_name"`
		Env           []string `json:"env"`
	}
	if err := json.Unmarshal(delBody, &got); err != nil {
		t.Fatalf("unmarshal delete body: %v\nbody: %s", err, delBody)
	}
	if got.ContainerName != "my-endpoint-0" {
		t.Errorf("container_name: got %q, want the inner container my-endpoint-0", got.ContainerName)
	}
	if len(got.Env) != 1 || got.Env[0] != "HF_HOME" {
		t.Errorf("env: got %v, want [HF_HOME]", got.Env)
	}
}

func TestEnvUnset_MultipleKeys(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "FOO", "BAR", "--deployment", "my-endpoint", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env unset failed: %v\nstderr:\n%s", err, stderr.String())
	}

	delBody := rec.getDelBody()
	if delBody == nil {
		t.Fatal("expected DELETE body")
	}
	var got struct {
		Env []string `json:"env"`
	}
	if err := json.Unmarshal(delBody, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Env) != 2 {
		t.Errorf("expected 2 env keys, got %d: %v", len(got.Env), got.Env)
	}
}

func TestEnvUnset_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "HF_HOME", "--deployment", "my-endpoint"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code: got %q, want CONFIRMATION_REQUIRED", agentErr.Code)
	}
}

func TestEnvList_Table_SecretMasking(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "bar"},
		{Name: "TOKEN", Type: "secret", ValueOrReferenceToSecret: "my-secret"},
	})
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "list", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env list failed: %v", err)
	}

	out := stdout.String()
	for _, want := range []string{"NAME", "TYPE", "VALUE", "FOO", "bar", "TOKEN", "***"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "my-secret") {
		t.Errorf("secret value should be masked, but found 'my-secret' in output:\n%s", out)
	}
}

func TestEnvSet_NonAgent_MissingFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "at least one --env or --env-secret") {
		t.Errorf("expected usage error about --env, got: %v", err)
	}
}

func TestEnvSet_JSONOutput(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "old"},
	})
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=new", "--env", "NEW=val"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["deployment"] != "my-endpoint" {
		t.Errorf("deployment: got %v, want my-endpoint", got["deployment"])
	}
	if got["added"] != float64(1) {
		t.Errorf("added: got %v, want 1", got["added"])
	}
	if got["updated"] != float64(1) {
		t.Errorf("updated: got %v, want 1", got["updated"])
	}
}

func TestEnvUnset_JSONOutput(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "HF_HOME", "MODEL", "--deployment", "my-endpoint", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env unset failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["deployment"] != "my-endpoint" {
		t.Errorf("deployment: got %v, want my-endpoint", got["deployment"])
	}
	if got["removed"] != float64(2) {
		t.Errorf("removed: got %v, want 2", got["removed"])
	}
}

func TestEnvSet_TableOutput(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, []verda.ContainerEnvVar{
		{Name: "FOO", Type: "plain", ValueOrReferenceToSecret: "old"},
	})
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "FOO=new", "--env", "NEW=val"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Updated environment variables") {
		t.Errorf("expected 'Updated environment variables' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "1 added") {
		t.Errorf("expected '1 added' in output; got:\n%s", out)
	}
	if !strings.Contains(out, "1 updated") {
		t.Errorf("expected '1 updated' in output; got:\n%s", out)
	}
}

func TestEnvUnset_TableOutput(t *testing.T) {
	t.Parallel()
	rec := newEnvRecordingServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "HF_HOME", "--deployment", "my-endpoint", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env unset failed: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Removed 1 environment variable") {
		t.Errorf("expected 'Removed 1 environment variable' in output; got:\n%s", stdout.String())
	}
}

func TestEnvUnset_AgentMode_RequiresDeployment(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "unset", "HF_HOME"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code: got %q, want MISSING_REQUIRED_FLAGS", agentErr.Code)
	}
}

// multiContainerGroups is the env GET body for the two-container deployment,
// in deployment order; tests pass other orders to show list keys on the
// deployment, not on group position.
var multiContainerGroups = []map[string]any{
	{"container_name": "my-endpoint-0", "env": []verda.ContainerEnvVar{{Name: "MAIN_ONLY", Type: "plain", ValueOrReferenceToSecret: "1"}}},
	{"container_name": "sidecar-1", "env": []verda.ContainerEnvVar{{Name: "SIDE_ONLY", Type: "plain", ValueOrReferenceToSecret: "2"}}},
}

// newMultiContainerServer serves a two-container deployment whose env groups
// differ, so tests can tell which group a command read.
func newMultiContainerServer(t *testing.T, groups []map[string]any) (*httptest.Server, *envRecordingServer) {
	t.Helper()
	rec := &envRecordingServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name:       "my-endpoint",
			Containers: []verda.DeploymentContainer{{Name: "my-endpoint-0"}, {Name: "sidecar-1"}},
		})
	})
	mux.HandleFunc("GET /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(groups)
	})
	mux.HandleFunc("POST /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.addBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PATCH /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.patchBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, rec
}

// list shows Containers[0] of the deployment, the same container set and
// unset write to, whatever order the env GET returns and even when that
// container has no group yet. The warning counts deployment containers.
func TestEnvList_MultiContainer_ShowsFirstAndWarns(t *testing.T) {
	t.Parallel()
	reversed := []map[string]any{multiContainerGroups[1], multiContainerGroups[0]}
	firstMissing := []map[string]any{multiContainerGroups[1]}
	cases := []struct {
		name   string
		groups []map[string]any
		want   []string
	}{
		{"deployment order", multiContainerGroups, []string{"MAIN_ONLY"}},
		{"reversed order", reversed, []string{"MAIN_ONLY"}},
		{"first container missing", firstMissing, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := newMultiContainerServer(t, tc.groups)
			f := newTestFactory(t, srv.URL)

			var stdout, stderr bytes.Buffer
			cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
			cmd.SetArgs([]string{"env", "list", "my-endpoint"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("env list failed: %v\nstderr:\n%s", err, stderr.String())
			}
			var got []struct{ Name string }
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout.String())
			}
			names := make([]string, 0, len(got))
			for _, v := range got {
				names = append(names, v.Name)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Errorf("want vars of my-endpoint-0 %v, got %v", tc.want, names)
			}
			if !strings.Contains(stderr.String(), `2 containers; showing "my-endpoint-0"`) {
				t.Errorf("want multi-container warning naming my-endpoint-0 on stderr, got %q", stderr.String())
			}
		})
	}
}

// set partitions add/update against the resolved container's group, not
// another container's: SIDE_ONLY exists only on the sidecar, so it is added.
func TestEnvSet_MultiContainer_UsesOwnGroup(t *testing.T) {
	t.Parallel()
	srv, rec := newMultiContainerServer(t, multiContainerGroups)
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"env", "set", "my-endpoint", "--env", "SIDE_ONLY=x", "--env", "MAIN_ONLY=y"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("env set failed: %v\nstderr:\n%s", err, stderr.String())
	}
	var add, patch struct {
		ContainerName string                  `json:"container_name"`
		Env           []struct{ Name string } `json:"env"`
	}
	if err := json.Unmarshal(rec.getAddBody(), &add); err != nil {
		t.Fatalf("unmarshal add: %v", err)
	}
	if err := json.Unmarshal(rec.getPatchBody(), &patch); err != nil {
		t.Fatalf("unmarshal patch: %v", err)
	}
	if add.ContainerName != "my-endpoint-0" || len(add.Env) != 1 || add.Env[0].Name != "SIDE_ONLY" {
		t.Errorf("add: want SIDE_ONLY on my-endpoint-0, got %+v", add)
	}
	if patch.ContainerName != "my-endpoint-0" || len(patch.Env) != 1 || patch.Env[0].Name != "MAIN_ONLY" {
		t.Errorf("patch: want MAIN_ONLY on my-endpoint-0, got %+v", patch)
	}
}
