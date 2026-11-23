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
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

type handlerFn = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)

// serverlessCalls enumerates every serverless handler with arguments that pass
// the argument checks and the confirm gate, so the call reaches the client.
func serverlessCalls(s *Server) []struct {
	tool string
	h    handlerFn
	args map[string]any
} {
	delSecret := s.deleteNamedHandler("delete_secret", func(ctx context.Context, c *verda.Client, name string, force bool) error {
		return c.ContainerDeployments.DeleteSecret(ctx, name, force)
	})
	return []struct {
		tool string
		h    handlerFn
		args map[string]any
	}{
		{"list_serverless_compute", s.handleListServerlessCompute, nil},
		{"list_registry_credentials", s.handleListRegistryCredentials, nil},
		{"list_secrets", s.handleListSecrets, nil},
		{"delete_secret", delSecret, map[string]any{"name": "x", "confirm": true}},
		{"list_containers", s.handleListContainers, nil},
		{"describe_container", s.handleDescribeContainer, map[string]any{"name": "api"}},
		{"list_container_replicas", s.handleListContainerReplicas, map[string]any{"name": "api"}},
		{"create_container", s.handleCreateContainer, containerCreateArgs(nil)},
		{"container_action", s.handleContainerAction, map[string]any{"name": "api", "action": "pause"}},
		{"container_action delete", s.handleContainerAction, map[string]any{"name": "api", "action": "delete", "confirm": true}},
		{"scale_container", s.handleScaleContainer, map[string]any{"name": "api", "confirm": true, "max_replicas": float64(2)}},
		{"get_container_env", s.handleGetContainerEnv, map[string]any{"name": "api"}},
		{"set_container_env", s.handleSetContainerEnv, map[string]any{"name": "api", "env": []any{"A=1"}}},
		{"unset_container_env", s.handleUnsetContainerEnv, map[string]any{"name": "api", "keys": []any{"A"}, "confirm": true}},
		{"list_batchjobs", s.handleListBatchjobs, nil},
		{"describe_batchjob", s.handleDescribeBatchjob, map[string]any{"name": "nightly"}},
		{"create_batchjob", s.handleCreateBatchjob, batchjobCreateArgs(nil)},
		{"batchjob_action", s.handleBatchjobAction, map[string]any{"name": "nightly", "action": "pause"}},
		{"batchjob_action delete", s.handleBatchjobAction, map[string]any{"name": "nightly", "action": "delete", "confirm": true}},
	}
}

// TestServerlessToolsSurfaceClientError: a latched credential failure reaches
// the agent as a tool error from every handler, never as a Go error or panic.
func TestServerlessToolsSurfaceClientError(t *testing.T) {
	t.Parallel()
	s := NewLazyServer(func() (*verda.Client, error) { return nil, errors.New("no credentials configured") })
	for _, c := range serverlessCalls(s) {
		res := call(t, c.h, c.tool, c.args)
		if !res.IsError || !strings.Contains(resultText(t, res), "no credentials configured") {
			t.Errorf("%s: want the client error as a tool error, got %s", c.tool, resultText(t, res))
		}
	}
}

// TestServerlessToolsSurfaceAPIError: an upstream 5xx on the first API call is
// a tool error, for every handler, with nothing created or changed.
func TestServerlessToolsSurfaceAPIError(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	for _, p := range []string{
		"/serverless-compute-resources", "/container-registry-credentials", "/secrets", "/secrets/x",
		"/container-deployments", "/container-deployments/api", "/container-deployments/api/replicas",
		"/container-deployments/api/pause", "/container-deployments/api/scaling",
		"/job-deployments", "/job-deployments/nightly", "/job-deployments/nightly/pause",
	} {
		mock.FailRoute(p, http.StatusBadGateway)
	}
	for _, c := range serverlessCalls(s) {
		res := call(t, c.h, c.tool, c.args)
		if !res.IsError {
			t.Errorf("%s: want a tool error on API failure, got %s", c.tool, resultText(t, res))
		}
	}
	if mock.HasContainer("my-api") || mock.HasJob("nightly") {
		t.Error("a failed create must not leave a deployment behind")
	}
}

func TestDescribeContainerBestEffortReads(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.FailRoute("/container-deployments/api/status", http.StatusServiceUnavailable)
	mock.FailRoute("/container-deployments/api/scaling", http.StatusServiceUnavailable)

	out := parseSuccess(t, call(t, s.handleDescribeContainer, "describe_container", map[string]any{"name": "api"}))
	if out["name"] != "api" {
		t.Fatalf("deployment must still be returned: %v", out)
	}
	if out["status_error"] == nil || out["scaling_error"] == nil {
		t.Errorf("failed reads must land in status_error / scaling_error: %v", out)
	}
	if _, has := out["status"]; has {
		t.Error("status must be omitted when the read failed")
	}

	// Same for the post-create status read.
	mock.FailRoute("/container-deployments/my-api/status", http.StatusServiceUnavailable)
	out = parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(nil)))
	if out["status_error"] == nil || !mock.HasContainer("my-api") {
		t.Errorf("create must succeed with status_error when the status read fails: %v", out)
	}

	mock.SeedJobDeployment("nightly")
	mock.FailRoute("/job-deployments/nightly/status", http.StatusServiceUnavailable)
	out = parseSuccess(t, call(t, s.handleDescribeBatchjob, "describe_batchjob", map[string]any{"name": "nightly"}))
	if out["status_error"] == nil || out["name"] != "nightly" {
		t.Errorf("batchjob describe must be best-effort on status: %v", out)
	}
}

// TestContainerActionWaitPermanentFailureIsError: a 4xx on the status read
// ends the wait at once and reports that the action itself was accepted.
func TestContainerActionWaitPermanentFailureIsError(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.FailRoute("/container-deployments/api/status", http.StatusForbidden)

	res := call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": "pause", "wait": true})
	if !res.IsError || !strings.Contains(resultText(t, res), "accepted but the wait failed") {
		t.Fatalf("a failed wait must say the action was accepted: %s", resultText(t, res))
	}
	if mock.ContainerStatus("api") != verda.ContainerDeploymentStatusPaused {
		t.Error("the pause itself must have gone through before the wait")
	}
	if n := mock.RequestCount(http.MethodGet, "/container-deployments/api/status"); n != 1 {
		t.Errorf("a permanent error must not be retried; status reads = %d", n)
	}
}

// TestContainerActionWaitToleratesTransientError: a 503 on one status read is
// "not yet", like scaling.Wait; the wait completes once reads recover.
func TestContainerActionWaitToleratesTransientError(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.FailRoute("/container-deployments/api/status", http.StatusServiceUnavailable)
	time.AfterFunc(200*time.Millisecond, mock.ClearFailures)

	out := parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": "pause", "wait": true}))
	if out["status"] != statusCompleted || out["deployment_status"] != verda.ContainerDeploymentStatusPaused {
		t.Fatalf("wait must recover from a transient read failure: %v", out)
	}
	if n := mock.RequestCount(http.MethodGet, "/container-deployments/api/status"); n < 2 {
		t.Errorf("the failed read must have been retried; status reads = %d", n)
	}
}

func TestContainerActionResumeWaitObservesStatus(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.SetContainerStatus("api", verda.ContainerDeploymentStatusPaused)

	out := parseSuccess(t, call(t, s.handleContainerAction, "container_action", map[string]any{"name": "api", "action": "resume", "wait": true}))
	if out["deployment_status"] != verda.ContainerDeploymentStatusHealthy || mock.ContainerStatus("api") != verda.ContainerDeploymentStatusHealthy {
		t.Fatalf("resume wait = %v", out)
	}
}

func TestContainerActionDeleteMissingIsError(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)
	res := call(t, s.handleContainerAction, "container_action", map[string]any{"name": "ghost", "action": "delete", "confirm": true})
	if !res.IsError {
		t.Fatal("deleting an unknown deployment must be a tool error")
	}
}

func TestScaleContainerWaitTimeoutKeepsAccepted(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.SetScalingReadLag(1000) // reads never catch up within the budget

	out := parseSuccess(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "max_replicas": float64(7), "wait": true, "wait_timeout_seconds": float64(1),
	}))
	if out["status"] != statusAccepted || out["wait_error"] == nil {
		t.Fatalf("a timed-out wait keeps accepted and reports wait_error: %v", out)
	}
	if _, has := out["current"]; has {
		t.Error("current must be absent when the wait did not confirm the change")
	}
	if got := dig(t, out, "expected", "max_replica_count"); got != float64(7) {
		t.Errorf("expected still carries the API answer: %v", got)
	}
}

func TestScaleContainerPatchFailureAfterPreRead(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.FailMethod(http.MethodPatch, "/container-deployments/api/scaling", http.StatusBadGateway)

	res := call(t, s.handleScaleContainer, "scale_container", map[string]any{"name": "api", "confirm": true, "max_replicas": float64(2)})
	if !res.IsError {
		t.Fatal("a failed PATCH must be a tool error")
	}
	if mock.RequestCount(http.MethodGet, "/container-deployments/api/scaling") != 1 {
		t.Error("the pre-read must have happened before the PATCH")
	}
}

// TestListContainersFilterFlagsUnreadableStatus: a row whose status cannot be
// read is kept with status_error, so a filtered list is never silently short.
func TestListContainersFilterFlagsUnreadableStatus(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("a")
	mock.SeedContainerDeployment("b")
	mock.SeedContainerDeployment("c")
	mock.SetContainerStatus("c", verda.ContainerDeploymentStatusPaused)
	mock.FailRoute("/container-deployments/a/status", http.StatusServiceUnavailable)

	var rows []map[string]any
	mustJSON(t, resultText(t, call(t, s.handleListContainers, "list_containers", map[string]any{"status": "healthy"})), &rows)
	if len(rows) != 2 || rows[0]["name"] != "a" || rows[1]["name"] != "b" {
		t.Fatalf("want the unreadable row a and the matching row b, got %v", rows)
	}
	if rows[0]["status_error"] == nil || rows[0]["status"] != nil {
		t.Errorf("unreadable row must carry status_error and no status: %v", rows[0])
	}
	if rows[1]["status"] != verda.ContainerDeploymentStatusHealthy || rows[1]["status_error"] != nil {
		t.Errorf("matching row = %v", rows[1])
	}
}

func TestSetContainerEnvWriteFailures(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)
	mock.FailMethod(http.MethodPost, envRoute, http.StatusBadGateway)
	res := call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{"name": "api", "env": []any{"NEW=1"}})
	if !res.IsError {
		t.Fatal("a failed add must be a tool error")
	}
	mock.FailMethod(http.MethodPatch, envRoute, http.StatusBadGateway)
	res = call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{"name": "api", "env": []any{"HF_HOME=/x"}})
	if !res.IsError || strings.Contains(resultText(t, res), "were added") {
		t.Fatalf("a failed update with nothing added is a plain error: %s", resultText(t, res))
	}
	if len(mock.ContainerEnv("api")) != 2 {
		t.Error("failed writes must leave the variables unchanged")
	}

	// Adds landed, then the update failed: the error must say so.
	s2, mock2 := seedEnv(t)
	mock2.FailMethod(http.MethodPatch, envRoute, http.StatusBadGateway)
	res = call(t, s2.handleSetContainerEnv, "set_container_env", map[string]any{"name": "api", "env": []any{"NEW=1", "HF_HOME=/x"}})
	if !res.IsError || !strings.Contains(resultText(t, res), "1 variable(s) were added but the update") {
		t.Fatalf("partial write must be reported: %s", resultText(t, res))
	}
	if len(mock2.ContainerEnv("api")) != 3 {
		t.Error("the add must have landed")
	}

	res = call(t, s.handleSetContainerEnv, "set_container_env", map[string]any{"name": "ghost", "env": []any{"A=1"}})
	if !res.IsError {
		t.Fatal("an unknown deployment must be a tool error")
	}
}

func TestUnsetContainerEnvFailures(t *testing.T) {
	t.Parallel()
	s, mock := seedEnv(t)
	res := call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{"name": "ghost", "keys": []any{"A"}, "confirm": true})
	if !res.IsError {
		t.Fatal("an unknown deployment must be a tool error")
	}
	mock.FailMethod(http.MethodDelete, envRoute, http.StatusBadGateway)
	res = call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{"name": "api", "keys": []any{"HF_HOME"}, "confirm": true})
	if !res.IsError || len(mock.ContainerEnv("api")) != 2 {
		t.Fatal("a failed DELETE must be a tool error and change nothing")
	}
	assertFieldError(t, call(t, s.handleUnsetContainerEnv, "unset_container_env", map[string]any{"name": "api", "keys": "HF_HOME", "confirm": true}), "keys")
}

func TestBatchjobActionFailures(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	res := call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "ghost", "action": "pause"})
	if !res.IsError {
		t.Fatal("pausing an unknown job must be a tool error")
	}
	res = call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "ghost", "action": "delete", "confirm": true})
	if !res.IsError {
		t.Fatal("deleting an unknown job must be a tool error")
	}

	mock.SeedJobDeployment("nightly")
	out := parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "pause"}))
	if out["status"] != statusAccepted || mock.JobStatus("nightly") != mockapi.JobStatusPaused {
		t.Fatalf("pause without wait = %v, mock status %q", out, mock.JobStatus("nightly"))
	}
	// A resumed job is "ready" when idle (the job vocabulary has no "healthy").
	out = parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "resume", "wait": true}))
	if out["status"] != statusCompleted || out["deployment_status"] != mockapi.JobStatusReady {
		t.Fatalf("resume wait = %v", out)
	}
	if n := mock.RequestCount(http.MethodGet, "/job-deployments/nightly/status"); n == 0 {
		t.Error("wait=true must poll the job status route")
	}
	// "running" is also a resumed job: the wait must accept it.
	mock.SetJobStatus("nightly", mockapi.JobStatusRunning)
	out = parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "resume", "wait": true}))
	if out["status"] != statusCompleted || out["deployment_status"] != mockapi.JobStatusRunning {
		t.Fatalf("resume wait on a running job = %v", out)
	}
	out = parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "delete", "confirm": true, "timeout_ms": float64(0)}))
	if out["status"] != statusAccepted {
		t.Fatalf("delete with timeout_ms 0 = %v, want accepted", out)
	}
}

func TestParseActionArgsTypeErrors(t *testing.T) {
	t.Parallel()
	_, _, err := parseActionArgs(map[string]any{"name": "a", "action": "pause", "wait": "yes"}, containerActions)
	assertFieldError(t, toolErrorResult(err), "wait")
	_, _, err = parseActionArgs(map[string]any{"name": "a", "action": "delete", "confirm": true, "timeout_ms": float64(-5)}, containerActions)
	assertFieldError(t, toolErrorResult(err), "timeout_ms")
	_, _, err = parseActionArgs(map[string]any{"action": "pause"}, containerActions)
	assertToolErrorCode(t, toolErrorResult(err), "MISSING_REQUIRED_FLAGS")

	in, action, err := parseActionArgs(map[string]any{"name": "a", "action": "delete", "confirm": true}, batchjobActions)
	if err != nil || in.timeoutMs != -1 || !action.destructive {
		t.Fatalf("delete defaults: in=%+v action=%+v err=%v", in, action, err)
	}
}

func TestParseScaleArgsEdges(t *testing.T) {
	t.Parallel()
	_, err := parseScaleArgs(map[string]any{"name": "a", "confirm": true, "max_replicas": float64(2), "wait": true, "wait_timeout_seconds": float64(0)})
	assertFieldError(t, toolErrorResult(err), "wait_timeout_seconds")
	_, err = parseScaleArgs(map[string]any{"name": "a", "confirm": true, "min_replicas": "1"})
	assertFieldError(t, toolErrorResult(err), "min_replicas")
	_, err = parseScaleArgs(map[string]any{"name": "a", "confirm": true, "queue_preset": "custom"})
	assertFieldError(t, toolErrorResult(err), "queue_preset")

	// An explicit queue_load wins over a preset; both are a single threshold.
	in, err := parseScaleArgs(map[string]any{"name": "a", "confirm": true, "queue_preset": "instant", "queue_load": 2.5})
	if err != nil || in.change.QueueLoad == nil || *in.change.QueueLoad != 2.5 {
		t.Fatalf("queue_load must win: %+v, %v", in, err)
	}
	in, err = parseScaleArgs(map[string]any{"name": "a", "confirm": true, "queue_preset": "instant", "cpu_util": float64(0)})
	if err != nil || *in.change.QueueLoad != 1 || in.change.CPUUtil == nil || *in.change.CPUUtil != 0 {
		t.Fatalf("preset instant → 1 and an explicit cpu_util 0 (off) must be kept: %+v, %v", in, err)
	}
}

func TestNewEnvViewWarnsOnMultiContainer(t *testing.T) {
	t.Parallel()
	v := newEnvView("d", &serverless.EnvTarget{Container: "d-0", Containers: 2})
	if v.Warning == "" || v.Env == nil || len(v.Env) != 0 {
		t.Fatalf("view = %+v: want a warning and an empty (not null) env", v)
	}
	if v := newEnvView("d", &serverless.EnvTarget{Container: "d-0", Containers: 1}); v.Warning != "" {
		t.Fatalf("single container must not warn: %+v", v)
	}
}

func TestServerlessArgErrorPassesAgentErrorsThrough(t *testing.T) {
	t.Parallel()
	ae := cmdutil.NewValidationError("deadline_seconds", "too short")
	if got := serverlessArgError(ae); !errors.Is(got, ae) {
		t.Fatalf("an AgentError must pass through unchanged, got %v", got)
	}
}

func TestCreateBatchjobExplicitOptions(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	parseSuccess(t, call(t, s.handleCreateBatchjob, "create_batchjob", batchjobCreateArgs(map[string]any{
		"registry_credentials": "ghcr",
		"max_replicas":         float64(8),
		"request_ttl_seconds":  float64(900),
		"entrypoint":           []any{"/bin/run"},
		"cmd":                  []any{"--fast"},
		"env_secrets":          []any{"TOKEN=hf"},
	})))
	b := mock.JobCreateBodies()[0]
	if got := dig(t, b, "container_registry_settings", "credentials", "name"); got != "ghcr" {
		t.Errorf("registry credentials = %v", got)
	}
	if got := dig(t, b, "scaling", "max_replica_count"); got != float64(8) {
		t.Errorf("max_replica_count = %v", got)
	}
	if got := dig(t, b, "scaling", "queue_message_ttl_seconds"); got != float64(900) {
		t.Errorf("request ttl = %v", got)
	}
	if got := dig(t, b, "containers", 0, "entrypoint_overrides", "cmd", 0); got != "--fast" {
		t.Errorf("cmd override = %v", got)
	}
}

func TestCreateContainerEntrypointAndHealthcheckOff(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(map[string]any{
		"healthcheck_off":        true,
		"entrypoint":             []any{"/bin/sh", "-c"},
		"registry_credentials":   "ghcr",
		"port":                   float64(8080),
		"concurrency":            float64(4),
		"request_ttl_seconds":    float64(60),
		"scale_up_delay_seconds": float64(10),
	})))
	b := mock.ContainerCreateBodies()[0]
	if _, has := dig(t, b, "containers", 0).(map[string]any)["healthcheck"]; has {
		t.Error("healthcheck_off must omit the healthcheck block")
	}
	if got := dig(t, b, "containers", 0, "entrypoint_overrides", "entrypoint", 1); got != "-c" {
		t.Errorf("entrypoint = %v", got)
	}
	if got := dig(t, b, "container_registry_settings", "is_private"); got != true {
		t.Errorf("is_private = %v", got)
	}
	if got := dig(t, b, "containers", 0, "exposed_port"); got != float64(8080) {
		t.Errorf("exposed_port = %v", got)
	}
	if got := dig(t, b, "scaling", "concurrent_requests_per_replica"); got != float64(4) {
		t.Errorf("concurrency = %v", got)
	}
	if got := dig(t, b, "scaling", "scale_up_policy", "delay_seconds"); got != float64(10) {
		t.Errorf("scale_up delay = %v", got)
	}

	parseSuccess(t, call(t, s.handleCreateContainer, "create_container", containerCreateArgs(map[string]any{
		"name": "probe", "healthcheck_path": "/ready", "healthcheck_port": float64(9000),
	})))
	b = mock.ContainerCreateBodies()[1]
	if got := dig(t, b, "containers", 0, "healthcheck", "path"); got != "/ready" {
		t.Errorf("healthcheck path = %v", got)
	}
	if got := dig(t, b, "containers", 0, "healthcheck", "port"); got != float64(9000) {
		t.Errorf("healthcheck port = %v", got)
	}
}
