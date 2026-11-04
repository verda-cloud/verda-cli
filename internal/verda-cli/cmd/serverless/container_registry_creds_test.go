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

type registryCredsServer struct {
	mu       sync.Mutex
	postBody []byte
	deleted  string
	srv      *httptest.Server
}

func newRegistryCredsServer(t *testing.T, creds []verda.RegistryCredentials) *registryCredsServer {
	t.Helper()
	rec := &registryCredsServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("GET /container-registry-credentials", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(creds)
	})

	mux.HandleFunc("POST /container-registry-credentials", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.postBody = body
		rec.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("DELETE /container-registry-credentials/{name}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.deleted = r.PathValue("name")
		rec.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func TestRegistryCredsList_Table(t *testing.T) {
	t.Parallel()
	creds := []verda.RegistryCredentials{
		{Name: "dockerhub-main", CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
		{Name: "gcr-prod", CreatedAt: time.Date(2026, 7, 15, 14, 30, 0, 0, time.UTC)},
	}
	rec := newRegistryCredsServer(t, creds)
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds list failed: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "CREATED") {
		t.Errorf("expected table header; got:\n%s", out)
	}
	if !strings.Contains(out, "dockerhub-main") {
		t.Errorf("expected dockerhub-main in output; got:\n%s", out)
	}
	if !strings.Contains(out, "gcr-prod") {
		t.Errorf("expected gcr-prod in output; got:\n%s", out)
	}
}

func TestRegistryCredsList_Empty(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, []verda.RegistryCredentials{})
	f := newTestFactory(t, rec.srv.URL)
	f.OutputFormatOverride = outputTable

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds list failed: %v", err)
	}

	if !strings.Contains(stdout.String(), "No registry credentials found.") {
		t.Errorf("expected empty message; got:\n%s", stdout.String())
	}
}

func TestRegistryCredsCreate_Dockerhub(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "dh",
		"--type", "dockerhub",
		"--username", "myuser",
		"--access-token", "pat123",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.postBody
	rec.mu.Unlock()
	if body == nil {
		t.Fatal("server did not receive POST /container-registry-credentials")
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got["name"] != "dh" {
		t.Errorf("name: got %v, want dh", got["name"])
	}
	if got["type"] != "dockerhub" {
		t.Errorf("type: got %v, want dockerhub", got["type"])
	}
	if got["username"] != "myuser" {
		t.Errorf("username: got %v, want myuser", got["username"])
	}
	if got["access_token"] != "pat123" {
		t.Errorf("access_token: got %v, want pat123", got["access_token"])
	}
}

func TestRegistryCredsCreate_GCR(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "gcr-creds",
		"--type", "gcr",
		"--service-account-key", `{"key":"val"}`,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.postBody
	rec.mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got["service_account_key"] != `{"key":"val"}` {
		t.Errorf("service_account_key: got %v", got["service_account_key"])
	}
}

func TestRegistryCredsCreate_AWSECR(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "ecr-creds",
		"--type", "aws-ecr",
		"--access-key-id", "AK",
		"--secret-access-key", "SK",
		"--region", "us-east-1",
		"--ecr-repo", "123.dkr.ecr.us-east-1.amazonaws.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.postBody
	rec.mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got["access_key_id"] != "AK" {
		t.Errorf("access_key_id: got %v", got["access_key_id"])
	}
	if got["secret_access_key"] != "SK" {
		t.Errorf("secret_access_key: got %v", got["secret_access_key"])
	}
	if got["region"] != "us-east-1" {
		t.Errorf("region: got %v", got["region"])
	}
	if got["ecr_repo"] != "123.dkr.ecr.us-east-1.amazonaws.com" {
		t.Errorf("ecr_repo: got %v", got["ecr_repo"])
	}
}

func TestRegistryCredsCreate_Custom(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "custom-creds",
		"--type", "custom",
		"--docker-config-json", `{"auths":{}}`,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	body := rec.postBody
	rec.mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if got["docker_config_json"] != `{"auths":{}}` {
		t.Errorf("docker_config_json: got %v", got["docker_config_json"])
	}
}

func TestRegistryCredsCreate_AgentMode_MissingFlags(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "create"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("expected MISSING_REQUIRED_FLAGS, got code=%q", agentErr.Code)
	}
}

func TestRegistryCredsDelete_WithYes(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "delete", "old-creds", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds delete failed: %v\nstderr:\n%s", err, stderr.String())
	}

	rec.mu.Lock()
	deleted := rec.deleted
	rec.mu.Unlock()
	if deleted != "old-creds" {
		t.Errorf("expected DELETE for old-creds, got %q", deleted)
	}
}

func TestRegistryCredsDelete_Force(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	// Extend the server to capture force param
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "delete", "old-creds", "--yes", "--force"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds delete --force failed: %v", err)
	}

	rec.mu.Lock()
	deleted := rec.deleted
	rec.mu.Unlock()
	if deleted != "old-creds" {
		t.Errorf("expected DELETE for old-creds, got %q", deleted)
	}
}

func TestRegistryCredsList_JSON(t *testing.T) {
	t.Parallel()
	creds := []verda.RegistryCredentials{
		{Name: "dockerhub-main", CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)},
	}
	rec := newRegistryCredsServer(t, creds)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds list failed: %v", err)
	}

	var got []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout.String())
	}
	if len(got) != 1 || got[0].Name != "dockerhub-main" {
		t.Errorf("expected dockerhub-main, got %+v", got)
	}
}

func TestRegistryCredsCreate_Verda(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "verda-creds",
		"--type", "verda",
		"--username", "myuser",
		"--access-token", "tok",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v", err)
	}

	rec.mu.Lock()
	body := rec.postBody
	rec.mu.Unlock()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["type"] != "verda" {
		t.Errorf("type: got %v, want verda", got["type"])
	}
	if got["username"] != "myuser" {
		t.Errorf("username: got %v, want myuser", got["username"])
	}
}

func TestRegistryCredsCreate_JSONOutput(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"registry-creds", "create",
		"--name", "dh",
		"--type", "dockerhub",
		"--username", "u",
		"--access-token", "t",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("registry-creds create failed: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if got["name"] != "dh" {
		t.Errorf("name: got %q, want dh", got["name"])
	}
	if got["type"] != "dockerhub" {
		t.Errorf("type: got %q, want dockerhub", got["type"])
	}
	if got["action"] != "created" {
		t.Errorf("action: got %q, want created", got["action"])
	}
}

func TestRegistryCredsDelete_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	rec := newRegistryCredsServer(t, nil)
	f := newTestFactory(t, rec.srv.URL)
	f.AgentModeOverride = true

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"registry-creds", "delete", "old-creds"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("expected CONFIRMATION_REQUIRED, got code=%q", agentErr.Code)
	}
}

func TestValidateRegistryCreds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		opts    *registryCredsOptions
		wantErr string
	}{
		{"valid dockerhub", &registryCredsOptions{Type: "dockerhub", Username: "u", AccessToken: "t"}, ""},
		{"valid ghcr", &registryCredsOptions{Type: "ghcr", Username: "u", AccessToken: "t"}, ""},
		{"valid verda", &registryCredsOptions{Type: "verda", Username: "u", AccessToken: "t"}, ""},
		{"valid scaleway", &registryCredsOptions{Type: "scaleway", Username: "u", AccessToken: "t"}, ""},
		{"valid gcr", &registryCredsOptions{Type: "gcr", ServiceAccountKey: "{}"}, ""},
		{"valid aws-ecr", &registryCredsOptions{Type: "aws-ecr", AccessKeyID: "AK", SecretAccessKey: "SK", Region: "us-east-1", ECRRepo: "repo"}, ""},
		{"valid custom", &registryCredsOptions{Type: "custom", DockerConfigJSON: "{}"}, ""},
		{"invalid type", &registryCredsOptions{Type: "invalid"}, "invalid --type"},
		{"dockerhub missing username", &registryCredsOptions{Type: "dockerhub", AccessToken: "t"}, "requires --username"},
		{"dockerhub missing token", &registryCredsOptions{Type: "dockerhub", Username: "u"}, "requires --username"},
		{"gcr missing key", &registryCredsOptions{Type: "gcr"}, "requires --service-account-key"},
		{"aws-ecr missing fields", &registryCredsOptions{Type: "aws-ecr", AccessKeyID: "AK"}, "requires --access-key-id"},
		{"custom missing config", &registryCredsOptions{Type: "custom"}, "requires --docker-config-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRegistryCreds(tc.opts)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}
