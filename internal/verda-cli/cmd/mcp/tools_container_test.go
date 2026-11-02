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
	"encoding/json"
	"net/http"
	"reflect"
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

	res, err := s.handleListContainerDeployments(t.Context(), callReq("list_container_deployments", nil))
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

	res, err := s.handleDescribeContainerDeployment(t.Context(),
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

	res, err := s.handleDescribeContainerDeployment(t.Context(),
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

	res, err := s.handleListContainerReplicas(t.Context(),
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

func scalingCall(t *testing.T, s *Server, a map[string]any) *mcp.CallToolResult {
	t.Helper()
	if _, ok := a["name"]; !ok {
		a["name"] = testDeployment
	}
	res, err := s.handleUpdateContainerScaling(t.Context(), callReq("update_container_scaling", a))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	return res
}

func TestUpdateContainerScalingConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	assertToolErrorCode(t, scalingCall(t, s, map[string]any{"min_replica_count": float64(2)}), "CONFIRMATION_REQUIRED")
	assertToolErrorCode(t, scalingCall(t, s, map[string]any{"min_replica_count": float64(2), "confirm": false}), "CONFIRMATION_REQUIRED")
	if n := len(mock.ScalingPatchBodies()); n != 0 {
		t.Fatalf("refused call reached the API: %d PATCH requests", n)
	}

	out := parseSuccess(t, scalingCall(t, s, map[string]any{"min_replica_count": float64(2), "confirm": true}))
	if out["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", out["status"])
	}
	prev, _ := out["previous"].(map[string]any)
	exp, _ := out["expected"].(map[string]any)
	if prev["min_replica_count"] != float64(0) || exp["min_replica_count"] != float64(2) {
		t.Errorf("previous/expected = %v / %v", prev, exp)
	}
	// Reads lag the PATCH; without wait nothing is reported as current.
	if _, ok := out["current"]; ok {
		t.Errorf("current reported without wait: %v", out["current"])
	}
	if got := mock.ContainerScaling(testDeployment).MinReplicaCount; got != 2 {
		t.Errorf("stored min = %d, want 2", got)
	}
}

// Wire-level: only the fields the agent set reach the API, and an explicit 0
// is sent rather than dropped as "unset".
func TestUpdateContainerScalingWireBody(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	parseSuccess(t, scalingCall(t, s, map[string]any{"min_replica_count": float64(2), "confirm": true}))
	parseSuccess(t, scalingCall(t, s, map[string]any{"min_replica_count": float64(0), "confirm": true}))
	parseSuccess(t, scalingCall(t, s, map[string]any{"cpu_utilization_threshold": float64(60), "scale_up_delay_seconds": float64(30), "confirm": true}))

	bodies := mock.ScalingPatchBodies()
	if len(bodies) != 3 {
		t.Fatalf("PATCH count = %d, want 3", len(bodies))
	}
	if v, ok := bodies[1]["min_replica_count"]; !ok || v != float64(0) || len(bodies[1]) != 1 {
		t.Errorf("min 0 body = %v, want exactly {min_replica_count: 0}", bodies[1])
	}
	want := `{"scale_up_policy":{"delay_seconds":30},"scaling_triggers":{"cpu_utilization":{"enabled":true,"threshold":60}}}`
	if got, _ := json.Marshal(bodies[2]); string(got) != want {
		t.Errorf("trigger body = %s, want %s", got, want)
	}
}

func TestUpdateContainerScalingTriggers(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	// Seeded GPU is already off: the disable is still sent, since reads lag.
	parseSuccess(t, scalingCall(t, s, map[string]any{"gpu_utilization_enabled": false, "confirm": true}))
	want := `{"scaling_triggers":{"gpu_utilization":{"enabled":false,"threshold":0}}}`
	if bodies := mock.ScalingPatchBodies(); len(bodies) != 1 {
		t.Fatalf("PATCH count = %d, want 1", len(bodies))
	} else if got, _ := json.Marshal(bodies[0]); string(got) != want {
		t.Errorf("disable body = %s, want %s", got, want)
	}

	// Enabling without a threshold reuses the stored one.
	parseSuccess(t, scalingCall(t, s, map[string]any{"cpu_utilization_enabled": true, "confirm": true}))
	if cpu := mock.ContainerScaling(testDeployment).ScalingTriggers.CPUUtilization; !cpu.Enabled || cpu.Threshold != 70 {
		t.Errorf("cpu after enable = %+v, want {true 70}", cpu)
	}

	parseSuccess(t, scalingCall(t, s, map[string]any{"queue_load_threshold": 1.5, "confirm": true}))
	if q := mock.ContainerScaling(testDeployment).ScalingTriggers.QueueLoad; q.Threshold != 1.5 {
		t.Errorf("queue load = %v, want 1.5", q.Threshold)
	}
}

func TestUpdateContainerScalingValidation(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)

	cases := []struct {
		name string
		args map[string]any
		code string
	}{
		{"no fields", map[string]any{"confirm": true}, "MISSING_REQUIRED_FLAGS"},
		{"max zero", map[string]any{"max_replica_count": float64(0), "confirm": true}, "VALIDATION_ERROR"},
		{"max over limit", map[string]any{"max_replica_count": float64(1001), "confirm": true}, "VALIDATION_ERROR"},
		{"util below 5", map[string]any{"cpu_utilization_threshold": float64(4), "confirm": true}, "VALIDATION_ERROR"},
		{"queue load below 1", map[string]any{"queue_load_threshold": 0.5, "confirm": true}, "VALIDATION_ERROR"},
		// Seeded max is 3: min alone must be checked against the current max.
		{"min above current max", map[string]any{"min_replica_count": float64(5), "confirm": true}, "VALIDATION_ERROR"},
		{"min above new max", map[string]any{"min_replica_count": float64(5), "max_replica_count": float64(4), "confirm": true}, "VALIDATION_ERROR"},
		// Seeded GPU is off with threshold 0: there is no threshold to reuse.
		{"enable without threshold", map[string]any{"gpu_utilization_enabled": true, "confirm": true}, "VALIDATION_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertToolErrorCode(t, scalingCall(t, s, tc.args), tc.code)
		})
	}
	if n := len(mock.ScalingPatchBodies()); n != 0 {
		t.Fatalf("invalid calls reached the API: %d PATCH requests", n)
	}
}

func actionCall(t *testing.T, s *Server, a map[string]any) *mcp.CallToolResult {
	t.Helper()
	a["name"] = testDeployment
	res, err := s.handleContainerAction(t.Context(), callReq("container_action", a))
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

func TestUpdateContainerScalingWait(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment(testDeployment)
	mock.SetScalingReadLag(1)

	out := parseSuccess(t, scalingCall(t, s, map[string]any{
		"max_replica_count": float64(5), "gpu_utilization_enabled": false, "confirm": true, "wait": true,
	}))
	if out["status"] != statusApplied {
		t.Fatalf("status = %v, want applied (wait_error: %v)", out["status"], out["wait_error"])
	}
	cur, _ := out["current"].(map[string]any)
	if !reflect.DeepEqual(cur, out["expected"]) {
		t.Errorf("current = %v, want expected %v", cur, out["expected"])
	}
	if cur["max_replica_count"] != float64(5) {
		t.Errorf("current max = %v, want 5", cur["max_replica_count"])
	}
}
