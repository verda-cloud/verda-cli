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

type describeJSON struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Scaling *struct {
		MaxReplicaCount int `json:"max_replica_count"`
	} `json:"scaling"`
	ScalingError string `json:"scaling_error"`
}

func TestContainerDescribe_Scaling(t *testing.T) {
	t.Run("table shows scaling", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, &cmdutil.TestFactory{ClientOverride: client}, "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		for _, want := range []string{"Scaling", "0..3", "balanced (3)", "on at 70%", "5m"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("json includes scaling", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, agentFactory(client), "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		var res describeJSON
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if res.Scaling == nil || res.Scaling.MaxReplicaCount != 3 || res.ScalingError != "" {
			t.Errorf("result = %+v, want scaling with max 3", res)
		}
	})

	t.Run("scaling failure is best-effort", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		mock.FailRoute("/container-deployments/"+testDeployment+"/scaling", http.StatusInternalServerError)

		stdout, _, err := runContainer(t, agentFactory(client), "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		var res describeJSON
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if res.Name != testDeployment || res.Status == "" || res.Scaling != nil || res.ScalingError == "" {
			t.Errorf("result = %+v, want the deployment and status with scaling_error", res)
		}

		stdout, _, err = runContainer(t, &cmdutil.TestFactory{ClientOverride: client}, "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe (table): %v", err)
		}
		if !strings.Contains(stdout, "Scaling") || !strings.Contains(stdout, "unavailable") {
			t.Errorf("table lacks the scaling note:\n%s", stdout)
		}
	})
}

func TestContainerDescribe_AgentRequiresName(t *testing.T) {
	t.Parallel()
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	_, _, err := runContainer(t, f, "describe")
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

func TestContainerDescribe_CardWithHealthcheck(t *testing.T) {
	t.Parallel()
	srv := newContainerDescribeHTTPServer(t, &verda.ContainerDeployment{
		Name:            "hc-deploy",
		EndpointBaseURL: "https://hc-deploy.run.verda.cloud",
		CreatedAt:       time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		Compute:         &verda.ContainerCompute{Name: "H100", Size: 1},
		Containers: []verda.DeploymentContainer{{
			Name:        "hc-deploy-0",
			Image:       verda.ContainerImage{Image: "myimg:v1"},
			ExposedPort: 8080,
			Healthcheck: &verda.ContainerHealthcheck{Enabled: true, Path: "/health", Port: 8080},
			Env: []verda.ContainerEnvVar{
				{Type: "plain", Name: "FOO", ValueOrReferenceToSecret: "bar"},
			},
			VolumeMounts: []verda.ContainerVolumeMount{
				{Type: "scratch", MountPath: "/data"},
			},
		}},
		ContainerRegistrySettings: &verda.ContainerRegistrySettings{
			IsPrivate:   true,
			Credentials: &verda.RegistryCredentialsRef{Name: "my-registry"},
		},
	}, "running")

	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable
	f.AgentModeOverride = false

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{"describe", "hc-deploy"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("describe: %v\nstderr: %s", err, stderr.String())
	}

	out := stdout.String()
	for _, want := range []string{"Healthcheck", "/health", "Env", "FOO", "Mounts", "scratch:/data", "Registry", "my-registry"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func newContainerDescribeHTTPServer(t *testing.T, deploy *verda.ContainerDeployment, status string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deploy)
	})

	mux.HandleFunc("GET /container-deployments/{name}/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeploymentStatus{Status: status})
	})

	mux.HandleFunc("GET /container-deployments/{name}/scaling", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&verda.ContainerScalingOptions{
			MinReplicaCount: 0,
			MaxReplicaCount: 3,
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}
