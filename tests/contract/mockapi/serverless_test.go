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

package mockapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// The mock must model the production edges the CLI and MCP tests rely on:
// env endpoints keyed on the inner container, duplicate/missing env names,
// duplicate deployments, and per-method failure overrides.

func do(t *testing.T, s *Server, method, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.URL()+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

func TestEnvRoutesKeyOnInnerContainer(t *testing.T) {
	t.Parallel()
	s := New()
	t.Cleanup(s.Close)
	s.SeedContainerDeployment("api")
	s.SetContainerEnv("api", []verda.ContainerEnvVar{{Type: "plain", Name: "A", ValueOrReferenceToSecret: "1"}})
	const route = "/container-deployments/api/environment-variables"

	// Deployment name as container_name is production's not_found.
	if code, _ := do(t, s, http.MethodPost, route, verda.ContainerEnvVarsRequest{ContainerName: "api", Env: []verda.ContainerEnvVar{{Name: "B"}}}); code != http.StatusNotFound {
		t.Errorf("POST with the deployment name = %d, want 404", code)
	}
	if code, _ := do(t, s, http.MethodPost, route, verda.ContainerEnvVarsRequest{ContainerName: "api-0", Env: []verda.ContainerEnvVar{{Name: "A"}}}); code != http.StatusBadRequest {
		t.Errorf("POST of an existing name = %d, want 400", code)
	}
	if code, _ := do(t, s, http.MethodPatch, route, verda.ContainerEnvVarsRequest{ContainerName: "api-0", Env: []verda.ContainerEnvVar{{Name: "ZZ"}}}); code != http.StatusNotFound {
		t.Errorf("PATCH of a missing name = %d, want 404", code)
	}
	if code, _ := do(t, s, http.MethodPost, route, "not json"); code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/container-deployments/ghost/environment-variables", nil); code != http.StatusNotFound {
		t.Errorf("unknown deployment = %d, want 404", code)
	}

	code, body := do(t, s, http.MethodGet, route, nil)
	var groups []envGroup
	if err := json.Unmarshal(body, &groups); err != nil || code != http.StatusOK {
		t.Fatalf("GET env = %d %s (%v)", code, body, err)
	}
	if len(groups) != 1 || groups[0].ContainerName != "api-0" || len(groups[0].Env) != 1 {
		t.Fatalf("grouped shape = %+v", groups)
	}
	if s.ContainerEnv("ghost") != nil {
		t.Error("ContainerEnv of an unknown deployment is nil")
	}
}

func TestDeploymentRoutesEdges(t *testing.T) {
	t.Parallel()
	s := New()
	t.Cleanup(s.Close)
	s.SeedContainerDeployment("api")
	s.SeedJobDeployment("job")

	if code, _ := do(t, s, http.MethodPost, "/container-deployments", verda.CreateDeploymentRequest{Name: "api"}); code != http.StatusConflict {
		t.Errorf("duplicate container create = %d, want 409", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/job-deployments", verda.CreateJobDeploymentRequest{Name: "job"}); code != http.StatusConflict {
		t.Errorf("duplicate job create = %d, want 409", code)
	}
	if code, _ := do(t, s, http.MethodPost, "/container-deployments", "nope"); code != http.StatusBadRequest {
		t.Errorf("bad create body = %d, want 400", code)
	}
	for _, p := range []string{"/container-deployments/ghost", "/job-deployments/ghost", "/job-deployments/ghost/status", "/container-deployments/ghost/pause", "/job-deployments/ghost/pause"} {
		method := http.MethodGet
		if p[len(p)-5:] == "pause" {
			method = http.MethodPost
		}
		if code, _ := do(t, s, method, p, nil); code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", method, p, code)
		}
	}
	if code, _ := do(t, s, http.MethodDelete, "/job-deployments/ghost", nil); code != http.StatusNotFound {
		t.Error("deleting an unknown job must be 404")
	}
	if s.ContainerStatus("ghost") != "" || s.JobStatus("ghost") != "" {
		t.Error("status helpers return \"\" for unknown names")
	}
	if code, _ := do(t, s, http.MethodDelete, "/secrets/ghost", nil); code != http.StatusNotFound {
		t.Error("deleting an unknown secret must be 404")
	}
}

func TestFailMethodIsScopedToTheMethod(t *testing.T) {
	t.Parallel()
	s := New()
	t.Cleanup(s.Close)
	s.SeedContainerDeployment("api")
	const route = "/container-deployments/api/scaling"
	s.FailMethod(http.MethodPatch, route, http.StatusBadGateway)

	if code, _ := do(t, s, http.MethodGet, route, nil); code != http.StatusOK {
		t.Errorf("GET must keep working: %d", code)
	}
	if code, _ := do(t, s, http.MethodPatch, route, map[string]any{"max_replica_count": 2}); code != http.StatusBadGateway {
		t.Errorf("PATCH must fail: %d", code)
	}
	if s.RequestCount(http.MethodGet, route) != 1 || s.RequestCount(http.MethodPatch, route) != 1 {
		t.Error("RequestCount must count failed requests too")
	}
	s.ClearFailures()
	if code, _ := do(t, s, http.MethodPatch, route, map[string]any{"max_replica_count": 2}); code != http.StatusOK {
		t.Errorf("after ClearFailures PATCH works: %d", code)
	}
}
