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
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

const testDeployment = "web"

func TestListContainerDeployments(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	res, err := s.handleListContainerDeployments(context.Background(), callReq("list_container_deployments", nil))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := resultText(t, res)
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not a JSON array: %v", err)
	}
	if len(out) != 1 || out[0]["name"] != testDeployment {
		t.Fatalf("unexpected list: %s", text)
	}
	if strings.Contains(text, mockapi.SeededEnvSecretValue) || strings.Contains(text, "API_TOKEN") {
		t.Errorf("list must not carry env vars: %s", text)
	}
}

func TestDescribeContainerDeployment(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	res, err := s.handleDescribeContainerDeployment(context.Background(),
		callReq("describe_container_deployment", map[string]any{"name": testDeployment}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := resultText(t, res)
	if strings.Contains(text, mockapi.SeededEnvSecretValue) {
		t.Fatalf("env var value leaked: %s", text)
	}
	out := parseSuccess(t, res)
	if out["status"] != "healthy" {
		t.Errorf("status = %v, want healthy", out["status"])
	}
	scaling, ok := out["scaling"].(map[string]any)
	if !ok || scaling["max_replica_count"] != float64(3) {
		t.Errorf("scaling missing or wrong: %v", out["scaling"])
	}
	containers, _ := out["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("containers = %v", out["containers"])
	}
	c, _ := containers[0].(map[string]any)
	if names, _ := c["env_names"].([]any); len(names) != 1 || names[0] != "API_TOKEN" {
		t.Errorf("env_names = %v, want [API_TOKEN]", c["env_names"])
	}
}

func TestDescribeContainerDeploymentBestEffort(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)
	mock.FailRoute("/container-deployments/"+testDeployment+"/status", http.StatusInternalServerError)
	mock.FailRoute("/container-deployments/"+testDeployment+"/scaling", http.StatusInternalServerError)

	res, err := s.handleDescribeContainerDeployment(context.Background(),
		callReq("describe_container_deployment", map[string]any{"name": testDeployment}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	if out["name"] != testDeployment {
		t.Errorf("deployment missing: %v", out)
	}
	if out["status_error"] == nil || out["scaling_error"] == nil {
		t.Errorf("want status_error and scaling_error, got %v", out)
	}
	if _, ok := out["scaling"]; ok {
		t.Errorf("scaling must be absent when its call failed: %v", out["scaling"])
	}
}

func TestListContainerReplicas(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	res, err := s.handleListContainerReplicas(context.Background(),
		callReq("list_container_replicas", map[string]any{"name": testDeployment}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	if out["count"] != float64(1) {
		t.Errorf("count = %v, want 1", out["count"])
	}
	counts, _ := out["counts_by_status"].(map[string]any)
	if counts["running"] != float64(1) {
		t.Errorf("counts_by_status = %v, want running:1", out["counts_by_status"])
	}
}

func actionCall(t *testing.T, s *Server, a map[string]any) *mcp.CallToolResult {
	t.Helper()
	a["name"] = testDeployment
	res, err := s.handleContainerAction(context.Background(), callReq("container_action", a))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestContainerActionConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	parseSuccess(t, actionCall(t, s, map[string]any{"action": "pause"}))
	if n := mock.ContainerActionCount(testDeployment, "pause"); n != 1 {
		t.Errorf("pause without confirm: %d calls, want 1", n)
	}
	for _, action := range []struct{ arg, path string }{{"restart", "restart"}, {"purge_queue", "purge-queue"}} {
		assertToolErrorCode(t, actionCall(t, s, map[string]any{"action": action.arg}), "CONFIRMATION_REQUIRED")
		assertToolErrorCode(t, actionCall(t, s, map[string]any{"action": action.arg, "confirm": false}), "CONFIRMATION_REQUIRED")
		if n := mock.ContainerActionCount(testDeployment, action.path); n != 0 {
			t.Fatalf("refused %s reached the API %d times", action.arg, n)
		}
		out := parseSuccess(t, actionCall(t, s, map[string]any{"action": action.arg, "confirm": true}))
		if out["status"] != "accepted" || mock.ContainerActionCount(testDeployment, action.path) != 1 {
			t.Errorf("%s with confirm: %v, %d calls", action.arg, out, mock.ContainerActionCount(testDeployment, action.path))
		}
	}
}

func TestContainerActionInvalid(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)

	assertToolErrorCode(t, actionCall(t, s, map[string]any{"action": "delete", "confirm": true}), "VALIDATION_ERROR")
}
