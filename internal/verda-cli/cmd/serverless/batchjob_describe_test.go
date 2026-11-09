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

func newBatchjobDescribeServer(t *testing.T, job *verda.JobDeployment, status string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("GET /job-deployments/{name}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(job)
	})

	mux.HandleFunc("GET /job-deployments/{name}/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.JobDeploymentStatus{Status: status})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBatchjobDescribe_JSON(t *testing.T) {
	t.Parallel()
	job := &verda.JobDeployment{
		Name:            "my-job",
		EndpointBaseURL: "https://jobs.datacrunch.io/my-job",
		CreatedAt:       time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		Compute:         &verda.ContainerCompute{Name: "H100", Size: 1},
		Containers: []verda.DeploymentContainer{{
			Name:        "my-job-0",
			Image:       verda.ContainerImage{Image: "myimg:v1"},
			ExposedPort: 8080,
		}},
		Scaling: &verda.JobScalingOptions{
			MaxReplicaCount:        5,
			DeadlineSeconds:        3600,
			QueueMessageTTLSeconds: 600,
		},
	}
	srv := newBatchjobDescribeServer(t, job, "running")
	f := newTestFactory(t, srv.URL)

	stdout, _, err := runBatchjob(t, f, "describe", "my-job")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	var got struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if got.Name != "my-job" {
		t.Errorf("name = %q, want my-job", got.Name)
	}
	if got.Status != "running" {
		t.Errorf("status = %q, want running", got.Status)
	}
}

func TestBatchjobDescribe_Table(t *testing.T) {
	t.Parallel()
	job := &verda.JobDeployment{
		Name:            "my-job",
		EndpointBaseURL: "https://jobs.datacrunch.io/my-job",
		CreatedAt:       time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		Compute:         &verda.ContainerCompute{Name: "H100", Size: 2},
		Containers: []verda.DeploymentContainer{{
			Name:        "my-job-0",
			Image:       verda.ContainerImage{Image: "myimg:v1"},
			ExposedPort: 8080,
			Env: []verda.ContainerEnvVar{
				{Type: "plain", Name: "KEY", ValueOrReferenceToSecret: "val"},
			},
		}},
		Scaling: &verda.JobScalingOptions{
			MaxReplicaCount:        10,
			DeadlineSeconds:        7200,
			QueueMessageTTLSeconds: 1200,
		},
	}
	srv := newBatchjobDescribeServer(t, job, "healthy")
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdBatchjob(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"describe", "my-job"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("describe: %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"my-job", "H100", "myimg:v1", "Endpoint", "Scaling"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestBatchjobDescribe_AgentRequiresName(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	_, _, err := runBatchjob(t, f, "describe")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q, want MISSING_REQUIRED_FLAGS", agentErr.Code)
	}
}
