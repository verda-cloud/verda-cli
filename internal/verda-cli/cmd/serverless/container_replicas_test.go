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
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newReplicasServer(t *testing.T, replicas verda.DeploymentReplicas) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("GET /container-deployments/{name}/replicas", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(replicas)
	})

	mux.HandleFunc("GET /container-deployments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]verda.ContainerDeployment{
			{Name: "my-endpoint"},
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestReplicas_Table(t *testing.T) {
	t.Parallel()
	srv := newReplicasServer(t, verda.DeploymentReplicas{
		List: []verda.ReplicaInfo{
			{
				ID:        "rep-001",
				Status:    "running",
				StartedAt: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
				Image:     "ghcr.io/org/app:v1.2",
			},
			{
				ID:        "rep-002",
				Status:    "initializing",
				StartedAt: time.Date(2026, 6, 15, 11, 0, 0, 0, time.UTC),
				ImageName: "nginx",
				ImageTag:  "1.21",
			},
		},
	})
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"replicas", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replicas failed: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"ID", "STATUS", "STARTED", "IMAGE", "rep-001", "rep-002", "running", "ghcr.io/org/app:v1.2", "nginx:1.21"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestReplicas_JSON(t *testing.T) {
	t.Parallel()
	srv := newReplicasServer(t, verda.DeploymentReplicas{
		List: []verda.ReplicaInfo{
			{
				ID:        "rep-abc",
				Status:    "running",
				StartedAt: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
				Image:     "ghcr.io/org/app:v2.0",
			},
		},
	})
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"replicas", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replicas failed: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "rep-abc") {
		t.Errorf("expected rep-abc in JSON output, got:\n%s", out)
	}
}

func TestReplicas_JSONStructure(t *testing.T) {
	t.Parallel()
	srv := newReplicasServer(t, verda.DeploymentReplicas{
		List: []verda.ReplicaInfo{
			{
				ID:        "rep-abc",
				Status:    "running",
				StartedAt: time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
				Image:     "ghcr.io/org/app:v2.0",
			},
			{
				ID:        "rep-def",
				Status:    "initializing",
				StartedAt: time.Date(2026, 6, 15, 11, 0, 0, 0, time.UTC),
				ImageName: "nginx",
				ImageTag:  "1.21",
			},
		},
	})
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"replicas", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replicas failed: %v", err)
	}

	var got struct {
		List []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Image  string `json:"image"`
		} `json:"list"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nstdout: %s", err, stdout.String())
	}
	if len(got.List) != 2 {
		t.Fatalf("expected 2 replicas, got %d", len(got.List))
	}
	if got.List[0].ID != "rep-abc" || got.List[0].Status != "running" {
		t.Errorf("replica[0]: got %+v", got.List[0])
	}
	if got.List[1].ID != "rep-def" || got.List[1].Status != "initializing" {
		t.Errorf("replica[1]: got %+v", got.List[1])
	}
}

func TestReplicas_Empty(t *testing.T) {
	t.Parallel()
	srv := newReplicasServer(t, verda.DeploymentReplicas{})
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"replicas", "my-endpoint"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("replicas failed: %v\nstderr:\n%s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "No replicas found") {
		t.Errorf("expected 'No replicas found' message, got:\n%s", stdout.String())
	}
}

func TestReplicas_AgentMode_RequiresName(t *testing.T) {
	t.Parallel()
	srv := newReplicasServer(t, verda.DeploymentReplicas{})
	f := newTestFactory(t, srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"replicas"})
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

func TestReplicas_ImageFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		replica   verda.ReplicaInfo
		wantImage string
	}{
		{
			"name+tag",
			verda.ReplicaInfo{ID: "r1", Status: "running", StartedAt: time.Now(), ImageName: "nginx", ImageTag: "1.21"},
			"nginx:1.21",
		},
		{
			"name only",
			verda.ReplicaInfo{ID: "r2", Status: "running", StartedAt: time.Now(), ImageName: "nginx"},
			"nginx",
		},
		{
			"full image takes priority",
			verda.ReplicaInfo{ID: "r3", Status: "running", StartedAt: time.Now(), Image: "full:v1", ImageName: "ignored", ImageTag: "ignored"},
			"full:v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newReplicasServer(t, verda.DeploymentReplicas{List: []verda.ReplicaInfo{tc.replica}})
			f := newTestFactory(t, srv.URL)
			f.OutputFormatOverride = outputTable
			f.AgentModeOverride = false

			var stdout, stderr bytes.Buffer
			cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
			cmd.SetArgs([]string{"replicas", "my-endpoint"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("replicas failed: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.wantImage) {
				t.Errorf("expected %q in output, got:\n%s", tc.wantImage, stdout.String())
			}
		})
	}
}
