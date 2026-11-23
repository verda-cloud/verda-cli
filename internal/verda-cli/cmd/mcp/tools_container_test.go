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
	"maps"
	"net/http"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

func containerCreateArgs(extra map[string]any) map[string]any {
	a := map[string]any{
		"name":    "my-api",
		"image":   "ghcr.io/org/app:v1",
		"compute": mockapi.ComputeH100,
		"confirm": true,
	}
	maps.Copy(a, extra)
	return a
}

func call(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), tool string, a map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := h(context.Background(), callReq(tool, a))
	if err != nil {
		t.Fatalf("%s handler error: %v", tool, err)
	}
	return res
}

func TestCreateContainerConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)

	a := containerCreateArgs(nil)
	delete(a, "confirm")
	assertToolErrorCode(t, call(t, s.handleCreateContainer, "create_container", a), "CONFIRMATION_REQUIRED")
	a["confirm"] = false
	assertToolErrorCode(t, call(t, s.handleCreateContainer, "create_container", a), "CONFIRMATION_REQUIRED")
	if n := mock.RequestCount(http.MethodPost, "/container-deployments"); n != 0 {
		t.Fatalf("refused create must have zero side effects; mock saw %d POSTs", n)
	}

	out := parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(nil)))
	if out["name"] != "my-api" || out["status"] != verda.ContainerDeploymentStatusInitializing {
		t.Fatalf("create result = %v", out)
	}
	if !mock.HasContainer("my-api") {
		t.Fatal("deployment not created")
	}
}

// TestCreateContainerWireDefaults: the MCP path goes through the CLI's
// Request(), so the scratch mount and the balanced preset arrive on the wire.
func TestCreateContainerWireDefaults(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(nil)))

	bodies := mock.ContainerCreateBodies()
	if len(bodies) != 1 {
		t.Fatalf("want 1 POST body, got %d", len(bodies))
	}
	b := bodies[0]
	if got := dig(t, b, "containers", 0, "volume_mounts", 0, "type"); got != "scratch" {
		t.Errorf("mount type = %v, want scratch", got)
	}
	if got := dig(t, b, "containers", 0, "volume_mounts", 0, "mount_path"); got != "/data" {
		t.Errorf("mount path = %v, want /data", got)
	}
	if got := dig(t, b, "scaling", "scaling_triggers", "queue_load", "threshold"); got != float64(3) {
		t.Errorf("balanced preset threshold = %v, want 3", got)
	}
	if got := dig(t, b, "compute", "size"); got != float64(1) {
		t.Errorf("compute_size default = %v, want 1", got)
	}
	if got := dig(t, b, "containers", 0, "healthcheck", "port"); got != float64(80) {
		t.Errorf("healthcheck port defaults to the exposed port: %v", got)
	}
}

func TestCreateContainerExplicitOptions(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(map[string]any{
		"min_replicas":             float64(0),
		"max_replicas":             float64(5),
		"queue_preset":             "cost-saver",
		"cpu_util":                 float64(70),
		"spot":                     true,
		"env":                      []any{"HF_HOME=/data/hf"},
		"env_secrets":              []any{"API_KEY=prod-key"},
		"secret_mounts":            []any{"certs:/etc/certs"},
		"scale_down_delay_seconds": float64(600),
	})))
	b := mock.ContainerCreateBodies()[0]
	if got := dig(t, b, "scaling", "min_replica_count"); got != float64(0) {
		t.Errorf("min_replica_count = %v, want 0", got)
	}
	if got := dig(t, b, "scaling", "max_replica_count"); got != float64(5) {
		t.Errorf("max_replica_count = %v, want 5", got)
	}
	if got := dig(t, b, "scaling", "scaling_triggers", "queue_load", "threshold"); got != float64(6) {
		t.Errorf("cost-saver threshold = %v, want 6", got)
	}
	if got := dig(t, b, "scaling", "scaling_triggers", "cpu_utilization", "threshold"); got != float64(70) {
		t.Errorf("cpu threshold = %v, want 70", got)
	}
	if got := dig(t, b, "scaling", "scale_down_policy", "delay_seconds"); got != float64(600) {
		t.Errorf("scale_down delay = %v, want 600", got)
	}
	if got := dig(t, b, "is_spot"); got != true {
		t.Errorf("is_spot = %v, want true", got)
	}
	if got := dig(t, b, "containers", 0, "env", 1, "type"); got != "secret" {
		t.Errorf("env_secrets type = %v, want secret", got)
	}
	if got := dig(t, b, "containers", 0, "volume_mounts", 1, "secret_name"); got != "certs" {
		t.Errorf("secret mount = %v, want certs", got)
	}
}

func TestCreateContainerValidationBeforeRequest(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	cases := []struct {
		name  string
		extra map[string]any
		field string
	}{
		{"latest tag", map[string]any{"image": "nginx:latest"}, "image"},
		{"bad name", map[string]any{"name": "My_API"}, "name"},
		{"string number", map[string]any{"max_replicas": "3"}, "max_replicas"},
		{"env not array", map[string]any{"env": "A=b"}, "env"},
		{"lowercase env", map[string]any{"env": []any{"lower=1"}}, "env"},
		{"util out of range", map[string]any{"cpu_util": float64(3)}, "cpu_util"},
		{"custom without load", map[string]any{"queue_preset": "custom"}, "queue_preset"},
		{"unknown preset", map[string]any{"queue_preset": "turbo"}, "queue_preset"},
		{"relative mount", map[string]any{"secret_mounts": []any{"certs:etc"}}, "secret_mounts"},
		{"delay too long", map[string]any{"scale_up_delay_seconds": float64(99999)}, "scale_up_delay_seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertFieldError(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(tc.extra)), tc.field)
		})
	}
	a := containerCreateArgs(nil)
	delete(a, "image")
	assertToolErrorCode(t, call(t, s.handleCreateContainer, "create_container", a), "MISSING_REQUIRED_FLAGS")
	if n := mock.RequestCount(http.MethodPost, "/container-deployments"); n != 0 {
		t.Fatalf("validation must fail before any request; mock saw %d POSTs", n)
	}
}

func TestContainerActionConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	for _, action := range []string{"restart", "purge_queue", "delete"} {
		res := call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": action})
		d := toolErrorDetails(t, res, "CONFIRMATION_REQUIRED")
		if d["action"] != action {
			t.Errorf("details.action = %v, want %s", d["action"], action)
		}
	}
	if n := mock.RequestCount(http.MethodPost, "/container-deployments/api/restart") +
		mock.RequestCount(http.MethodPost, "/container-deployments/api/purge-queue") +
		mock.RequestCount(http.MethodDelete, "/container-deployments/api"); n != 0 {
		t.Fatalf("refused actions must have zero side effects; mock saw %d", n)
	}

	// pause/resume are not destructive: no confirm needed, accepted by default.
	out := parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": "pause"}))
	if out["status"] != statusAccepted {
		t.Fatalf("pause without wait must be accepted: %v", out)
	}

	res := call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": "explode"})
	assertFieldError(t, res, "action")
}

func TestContainerActionWaitCompletes(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	out := parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{
		"name": "api", "action": "pause", "wait": true,
	}))
	if out["status"] != statusCompleted || out["deployment_status"] != verda.ContainerDeploymentStatusPaused {
		t.Fatalf("pause wait = %v", out)
	}
	if n := mock.RequestCount(http.MethodGet, "/container-deployments/api/status"); n == 0 {
		t.Fatal("wait=true must poll the status route")
	}

	// restart has no observable target: wait is ignored, status stays accepted.
	out = parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{
		"name": "api", "action": "restart", "confirm": true, "wait": true,
	}))
	if out["status"] != statusAccepted {
		t.Fatalf("restart must report accepted: %v", out)
	}
}

func TestContainerActionDelete(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.SeedContainerDeployment("api2")

	out := parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{
		"name": "api", "action": "delete", "confirm": true,
	}))
	if out["status"] != statusCompleted || mock.HasContainer("api") {
		t.Fatalf("delete with the server-side wait must report completed: %v", out)
	}

	out = parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{
		"name": "api2", "action": "delete", "confirm": true, "timeout_ms": float64(0),
	}))
	if out["status"] != statusAccepted {
		t.Fatalf("delete with timeout_ms 0 returns before the deployment is gone: %v", out)
	}
	q := mock.DeleteQueries()
	if len(q) != 2 || q[0] != "" || q[1] != "timeout=0" {
		t.Fatalf("delete queries = %q, want [\"\" \"timeout=0\"]", q)
	}
}

func TestDescribeContainer(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	out := parseSuccess(t, call(t, s.handleDescribeContainer, "describe_container", map[string]any{"name": "api"}))
	if out["status"] != verda.ContainerDeploymentStatusHealthy {
		t.Errorf("status = %v", out["status"])
	}
	if got := dig(t, out, "scaling", "max_replica_count"); got != float64(3) {
		t.Errorf("scaling.max_replica_count = %v, want 3", got)
	}
	if got := dig(t, out, "containers", 0, "name"); got != "api-0" {
		t.Errorf("inner container = %v, want api-0", got)
	}

	assertToolErrorCode(t, call(t, s.handleDescribeContainer, "describe_container", map[string]any{}), "MISSING_REQUIRED_FLAGS")
}

func TestListContainersStatusFilter(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("a")
	mock.SeedContainerDeployment("b")
	mock.SetContainerStatus("b", verda.ContainerDeploymentStatusPaused)

	res := call(t, s.handleListContainers, "list_containers", map[string]any{})
	var all []map[string]any
	mustJSON(t, resultText(t, res), &all)
	if len(all) != 2 {
		t.Fatalf("unfiltered list = %d rows, want 2", len(all))
	}
	if n := mock.RequestCount(http.MethodGet, "/container-deployments/a/status"); n != 0 {
		t.Fatal("unfiltered list must not read per-row status")
	}

	res = call(t, s.handleListContainers, "list_containers", map[string]any{"status": "PAUSED"})
	var paused []map[string]any
	mustJSON(t, resultText(t, res), &paused)
	if len(paused) != 1 || paused[0]["name"] != "b" || paused[0]["status"] != verda.ContainerDeploymentStatusPaused {
		t.Fatalf("filtered list = %v", paused)
	}
}

func TestListContainerReplicas(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	out := parseSuccess(t, call(t, s.handleListContainerReplicas, "list_container_replicas", map[string]any{"name": "api"}))
	if out["count"] != float64(1) {
		t.Errorf("count = %v, want 1", out["count"])
	}
	if got := dig(t, out, "counts_by_status", verda.ReplicaStatusRunning); got != float64(1) {
		t.Errorf("counts_by_status.running = %v, want 1", got)
	}
}
