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
	"net/http"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

const envRoute = "/container-deployments/api/environment-variables"

func seedEnv(t *testing.T) (*Server, *mockapi.Server) {
	t.Helper()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.SetContainerEnv("api", []verda.ContainerEnvVar{
		{Type: "plain", Name: "HF_HOME", ValueOrReferenceToSecret: "/data/hf"},
		{Type: "secret", Name: "API_KEY", ValueOrReferenceToSecret: "prod-key"},
	})
	return s, mock
}

func TestGetContainerEnv(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)

	out := parseSuccess(t, call(t, s.handleGetContainerEnv, "get_container_env", map[string]any{"name": "api"}))
	if out["container"] != "api-0" || out["containers"] != float64(1) {
		t.Fatalf("env target = %v", out)
	}
	if got := dig(t, out, "env", 1, "name"); got != "API_KEY" {
		t.Errorf("env[1].name = %v", got)
	}
	if _, warned := out["warning"]; warned {
		t.Error("single-container deployment must not warn")
	}
	if mock.RequestCount(http.MethodGet, envRoute) != 1 {
		t.Error("get must read the grouped env route once")
	}
}

func TestSetContainerEnvAddsAndUpdates(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)

	out := parseSuccess(t, call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{
		"name":        "api",
		"env":         []any{"HF_HOME=/models", "MODEL=llama"},
		"env_secrets": []any{"TOKEN=hf-token"},
	}))
	if out["added"] != float64(2) || out["updated"] != float64(1) {
		t.Fatalf("added/updated = %v/%v, want 2/1", out["added"], out["updated"])
	}
	env := mock.ContainerEnv("api")
	if len(env) != 4 {
		t.Fatalf("mock env = %v, want 4 vars", env)
	}
	if env[0].ValueOrReferenceToSecret != "/models" {
		t.Errorf("HF_HOME not updated in place: %v", env[0])
	}
	// One POST for the new names, one PATCH for the existing one; both keyed on the inner container.
	if mock.RequestCount(http.MethodPost, envRoute) != 1 || mock.RequestCount(http.MethodPatch, envRoute) != 1 {
		t.Errorf("POST/PATCH counts = %d/%d, want 1/1",
			mock.RequestCount(http.MethodPost, envRoute), mock.RequestCount(http.MethodPatch, envRoute))
	}
	if got := dig(t, out, "env", 3, "type"); got != "secret" {
		t.Errorf("post-write read lacks the secret var: %v", out["env"])
	}
}

func TestSetContainerEnvValidation(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)

	res := call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{"name": "api"})
	assertToolErrorCode(t, res, "MISSING_REQUIRED_FLAGS")

	assertFieldError(t, call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{
		"name": "api", "env": []any{"lower=1"},
	}), "env")
	assertFieldError(t, call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{
		"name": "api", "env": "A=1",
	}), "env")
	if mock.RequestCount(http.MethodPost, envRoute)+mock.RequestCount(http.MethodPatch, envRoute) != 0 {
		t.Fatal("invalid input must not write")
	}
}

func TestUnsetContainerEnvConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)

	res := call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{"name": "api", "keys": []any{"HF_HOME"}})
	assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")
	if mock.RequestCount(http.MethodDelete, envRoute) != 0 {
		t.Fatal("refused unset must not DELETE")
	}
	res = call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{"name": "api", "keys": []any{}, "confirm": true})
	assertToolErrorCode(t, res, "MISSING_REQUIRED_FLAGS")

	// removed is observed (before minus after), not the number of keys requested.
	out := parseSuccess(t, call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{
		"name": "api", "keys": []any{"HF_HOME", "NOT_THERE"}, "confirm": true,
	}))
	if out["removed"] != float64(1) {
		t.Fatalf("removed = %v, want 1 (one of two keys existed)", out["removed"])
	}
	if env := mock.ContainerEnv("api"); len(env) != 1 || env[0].Name != "API_KEY" {
		t.Fatalf("mock env after unset = %v", env)
	}
}
