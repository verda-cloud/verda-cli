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
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
)

const containerActionDelete = "delete"

func (s *Server) registerContainerTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_containers",
			mcp.WithDescription("List serverless container deployments (always-on HTTP endpoints). The list has no status; pass status to filter, which reads each deployment's status (exact, case-insensitive: healthy, unhealthy, paused, initializing, ...). A row whose status could not be read is still returned, without status and with status_error, so the filtered list is never silently incomplete. Use describe_container for one deployment's status and scaling."),
			mcp.WithString("status", mcp.Description("Filter by exact status, case-insensitive (e.g. healthy, paused)")),
		),
		s.handleListContainers,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("describe_container",
			mcp.WithDescription("Describe a serverless container deployment: image, compute, env var names, mounts, plus status and scaling options. Status and scaling reads are best-effort: a failure lands in status_error / scaling_error, the deployment is still returned. Plain env values are returned verbatim; secret-backed vars show only the secret name."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
		),
		s.handleDescribeContainer,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_container_replicas",
			mcp.WithDescription("List the replicas of a container deployment with per-status counts. Use after scale_container to watch replicas start or stop."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
		),
		s.handleListContainerReplicas,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_container",
			mcp.WithDescription("Create a serverless container deployment (starts billing while replicas run; min_replicas > 0 bills continuously). REQUIRES confirm: true — show the user name/image/compute/scaling first; without it the tool fails with CONFIRMATION_REQUIRED. Required: name (URL slug [a-z0-9-], max 63, immutable), image (must carry a specific tag, :latest is rejected), compute (from list_serverless_compute), confirm. Every deployment gets a server-sized scratch mount at /data. Returns the deployment plus its first status read (typically initializing); it does not wait for healthy."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name: lowercase alphanumerics and hyphens, max 63 chars; becomes https://containers.datacrunch.io/<name>")),
			mcp.WithString("image", mcp.Required(), mcp.Description("Container image with a specific tag, e.g. ghcr.io/org/app:v1.2 (:latest is rejected)")),
			mcp.WithString("compute", mcp.Required(), mcp.Description("Compute resource name from list_serverless_compute, e.g. H100 or RTX4500Ada")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithNumber("compute_size", mcp.Description("GPUs or vCPU cores per replica (default 1)")),
			mcp.WithBoolean("spot", mcp.Description("Run on spot compute (containers only; cheaper, may be preempted)")),
			mcp.WithNumber("port", mcp.Description("Exposed HTTP port (default 80)")),
			mcp.WithString("registry_credentials", mcp.Description("Registry credentials name from list_registry_credentials; omit for a public image")),
			mcp.WithBoolean("healthcheck_off", mcp.Description("Disable the HTTP health probe; requests route immediately")),
			mcp.WithString("healthcheck_path", mcp.Description("Health probe path (default /health)")),
			mcp.WithNumber("healthcheck_port", mcp.Description("Health probe port (default: the exposed port)")),
			mcp.WithArray("env", mcp.WithStringItems(), mcp.Description("Plain env vars as KEY=VALUE (KEY uppercase). Values are visible in describe output; use env_secrets for anything sensitive.")),
			mcp.WithArray("env_secrets", mcp.WithStringItems(), mcp.Description("Secret-backed env vars as KEY=SECRET_NAME (secret from list_secrets)")),
			mcp.WithArray("entrypoint", mcp.WithStringItems(), mcp.Description("Override the image ENTRYPOINT (one array element per argument)")),
			mcp.WithArray("cmd", mcp.WithStringItems(), mcp.Description("Override the image CMD (one array element per argument)")),
			mcp.WithArray("secret_mounts", mcp.WithStringItems(), mcp.Description("File secrets to mount as SECRET:/absolute/path")),
			mcp.WithNumber("min_replicas", mcp.Description("Minimum replicas (default 0 = scale to zero; above 0 bills with no traffic)")),
			mcp.WithNumber("max_replicas", mcp.Description("Maximum replicas (default 3)")),
			mcp.WithNumber("concurrency", mcp.Description("Concurrent requests per replica (default 1; raise for LLM serving)")),
			mcp.WithString("queue_preset", mcp.Description("Queue-load scaling preset: instant (threshold 1), balanced (3, default), cost-saver (6) or custom (requires queue_load)")),
			mcp.WithNumber("queue_load", mcp.Description("Custom queue-load threshold 1..1000; implies queue_preset custom")),
			mcp.WithNumber("cpu_util", mcp.Description("CPU utilization trigger threshold 5..100 (omit or 0 = off)")),
			mcp.WithNumber("gpu_util", mcp.Description("GPU utilization trigger threshold 5..100 (omit or 0 = off; rejected on CPU compute)")),
			mcp.WithNumber("scale_up_delay_seconds", mcp.Description("Delay before scaling up, 0..3600 (default 0)")),
			mcp.WithNumber("scale_down_delay_seconds", mcp.Description("Delay before scaling down, 0..3600 (default 300)")),
			mcp.WithNumber("request_ttl_seconds", mcp.Description("How long a queued request may wait before it is dropped, 0..86400 (default 300)")),
		),
		s.handleCreateContainer,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("container_action",
			mcp.WithDescription("Run a lifecycle action on a container deployment: pause, resume, restart, purge_queue or delete. restart, purge_queue and delete are destructive (in-flight requests are lost) and REQUIRE confirm: true. Returns status \"accepted\" (the API accepted the action) unless wait: true, which for pause/resume polls the deployment status (up to 5 min) and returns \"completed\"; restart and purge_queue are always \"accepted\". delete waits server-side for timeout_ms (default 60 s) and returns \"completed\", or \"accepted\" with timeout_ms 0."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
			mcp.WithString("action", mcp.Required(), mcp.Description("One of: pause, resume, restart, purge_queue, delete")),
			mcp.WithBoolean("confirm", mcp.Description("Set true to confirm destructive actions (restart, purge_queue, delete). Not needed for pause/resume.")),
			mcp.WithBoolean("wait", mcp.Description("pause/resume only: poll until the deployment reports paused/healthy (default false)")),
			mcp.WithNumber("timeout_ms", mcp.Description("delete only: server-side wait in ms, 0 returns immediately, max 300000 (default 60000)")),
		),
		s.handleContainerAction,
	)

	s.registerContainerScaleTool()
	s.registerContainerEnvTools()
}

// containerView adds the status and scaling reads to the SDK deployment, like `verda container describe -o json`.
type containerView struct {
	*verda.ContainerDeployment
	Status       string                         `json:"status,omitempty"`
	StatusError  string                         `json:"status_error,omitempty"`
	Scaling      *verda.ContainerScalingOptions `json:"scaling,omitempty"`
	ScalingError string                         `json:"scaling_error,omitempty"`
}

func containerStatusReader(client *verda.Client, name string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		st, err := client.ContainerDeployments.GetDeploymentStatus(ctx, name)
		if err != nil {
			return "", err
		}
		return st.Status, nil
	}
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListContainers(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status, err := optionalString(args(req), "status")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	deployments, err := client.ContainerDeployments.GetDeployments(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if status == "" {
		return jsonResult(deployments)
	}

	return jsonResult(filterContainersByStatus(ctx, client, deployments, status))
}

// statusReadParallelism bounds the per-row status reads of a filtered list so
// they share the --timeout budget concurrently instead of serially.
const statusReadParallelism = 8

// filterContainersByStatus: the list endpoint carries no status, so a filter
// needs one read per row (the N+1 the CLI accepts too). Rows whose status
// could not be read are kept with status_error set: an agent asking for
// paused deployments must be able to see that the answer is incomplete.
func filterContainersByStatus(ctx context.Context, client *verda.Client, deployments []verda.ContainerDeployment, status string) []containerView {
	type read struct{ status, err string }
	reads := make([]read, len(deployments))
	sem := make(chan struct{}, statusReadParallelism)
	var wg sync.WaitGroup
	for i := range deployments {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			reads[i].status, reads[i].err = readStatus(ctx, containerStatusReader(client, deployments[i].Name))
		})
	}
	wg.Wait()

	rows := make([]containerView, 0, len(deployments))
	for i := range deployments {
		switch {
		case reads[i].err != "":
			rows = append(rows, containerView{ContainerDeployment: &deployments[i], StatusError: reads[i].err})
		case strings.EqualFold(reads[i].status, status):
			rows = append(rows, containerView{ContainerDeployment: &deployments[i], Status: reads[i].status})
		}
	}
	return rows
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleDescribeContainer(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := requiredString(args(req), "name")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	d, err := client.ContainerDeployments.GetDeploymentByName(ctx, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := containerView{ContainerDeployment: d}
	view.Status, view.StatusError = readStatus(ctx, containerStatusReader(client, name))
	scalingCtx, scalingCancel := context.WithTimeout(ctx, statusReadTimeout)
	defer scalingCancel()
	if sc, scErr := client.ContainerDeployments.GetDeploymentScaling(scalingCtx, name); scErr != nil {
		view.ScalingError = scErr.Error()
	} else {
		view.Scaling = sc
	}
	return jsonResult(view)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListContainerReplicas(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := requiredString(args(req), "name")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	replicas, err := client.ContainerDeployments.GetDeploymentReplicas(ctx, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	counts := map[string]int{}
	for i := range replicas.List {
		counts[replicas.List[i].Status]++
	}
	return jsonResult(map[string]any{
		"name":             name,
		"count":            len(replicas.List),
		"counts_by_status": counts,
		"replicas":         replicas.List,
	})
}

// containerCreateOptions maps MCP arguments onto the CLI's create options;
// defaults come from NewContainerCreateOptions, so both surfaces agree.
func containerCreateOptions(a map[string]any) (*serverless.ContainerCreateOptions, bool, error) {
	r := &argReader{a: a}
	o := serverless.NewContainerCreateOptions()
	o.Name = r.reqStr("name")
	o.Image = r.reqStr("image")
	o.Compute = r.reqStr("compute")
	confirm := r.boolean("confirm")
	setInt(&o.ComputeSize, r.intPtr("compute_size"))
	o.Spot = r.boolean("spot")
	setInt(&o.Port, r.intPtr("port"))
	o.RegistryCreds = r.str("registry_credentials")
	o.HealthcheckOff = r.boolean("healthcheck_off")
	if p := r.str("healthcheck_path"); p != "" {
		o.HealthcheckPath = p
	}
	setInt(&o.HealthcheckPort, r.intPtr("healthcheck_port"))
	o.Env = r.strs("env")
	o.EnvSecret = r.strs("env_secrets")
	o.Entrypoint = r.strs("entrypoint")
	o.Cmd = r.strs("cmd")
	o.SecretMounts = r.strs("secret_mounts")
	setInt(&o.MinReplicas, r.intPtr("min_replicas"))
	setInt(&o.MaxReplicas, r.intPtr("max_replicas"))
	setInt(&o.Concurrency, r.intPtr("concurrency"))
	if p := r.enum("queue_preset", serverless.PresetInstant, serverless.PresetBalanced, serverless.PresetCostSaver, serverless.PresetCustom); p != "" {
		o.QueuePreset = p
	}
	setInt(&o.QueueLoad, r.intPtr("queue_load"))
	setInt(&o.CPUUtil, r.intPtr("cpu_util"))
	setInt(&o.GPUUtil, r.intPtr("gpu_util"))
	r.seconds(&o.ScaleUpDelay, "scale_up_delay_seconds")
	r.seconds(&o.ScaleDownDelay, "scale_down_delay_seconds")
	r.seconds(&o.RequestTTL, "request_ttl_seconds")
	return o, confirm, r.err
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleCreateContainer(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	opts, confirm, err := containerCreateOptions(args(req))
	if err != nil {
		return toolErrorResult(err), nil
	}
	// Request() validates and assembles offline (scratch mount, preset math,
	// :latest, SDK validation): argument errors surface before the confirm gate
	// so the agent fixes them before asking the user.
	createReq, err := opts.Request()
	if err != nil {
		return toolErrorResult(serverlessArgError(err)), nil
	}
	if !confirm {
		return toolErrorResult(confirmationRequiredError("create_container")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	d, err := client.ContainerDeployments.CreateDeployment(ctx, createReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := containerView{ContainerDeployment: d}
	view.Status, view.StatusError = readStatus(ctx, containerStatusReader(client, d.Name))
	return jsonResult(view)
}

// deploymentAction describes a lifecycle verb; delete is handled apart (timeout_ms, server-side wait).
type deploymentAction struct {
	expectStatuses []string // any of these ends wait=true; empty = never polled
	destructive    bool     // requires confirm=true
	exec           func(ctx context.Context, c *verda.Client, name string) error
}

// containerActions mirrors cmd/serverless/container_actions.go: restart and
// purge-queue are destructive there (they break in-flight requests), pause and
// resume are not. Keep both in sync.
var containerActions = map[string]deploymentAction{
	"pause": {
		expectStatuses: []string{verda.ContainerDeploymentStatusPaused},
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.PauseDeployment(ctx, name)
		},
	},
	"resume": {
		expectStatuses: []string{verda.ContainerDeploymentStatusHealthy},
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.ResumeDeployment(ctx, name)
		},
	},
	"restart": {
		destructive: true,
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.RestartDeployment(ctx, name)
		},
	},
	"purge_queue": {
		destructive: true,
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.PurgeDeploymentQueue(ctx, name)
		},
	},
	containerActionDelete: {destructive: true},
}

func actionNames(actions map[string]deploymentAction) string {
	names := make([]string, 0, len(actions))
	for name := range actions {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// actionArgs is the parsed, gate-checked argument set shared by container_action and batchjob_action.
type actionArgs struct {
	name, action string
	wait         bool
	timeoutMs    int
}

// parseActionArgs reads the arguments and runs the confirm gate before any API call.
func parseActionArgs(a map[string]any, actions map[string]deploymentAction) (*actionArgs, *deploymentAction, error) {
	r := &argReader{a: a}
	out := &actionArgs{name: r.reqStr("name"), action: r.reqStr("action"), timeoutMs: -1}
	confirm := r.boolean("confirm")
	out.wait = r.boolean("wait")
	setInt(&out.timeoutMs, r.intPtr("timeout_ms"))
	if r.err != nil {
		return nil, nil, r.err
	}
	action, ok := actions[out.action]
	if !ok {
		return nil, nil, invalidArgError("action", fmt.Sprintf("invalid value %q (valid: %s)", out.action, actionNames(actions)))
	}
	if action.destructive && !confirm {
		return nil, nil, confirmationRequiredError(out.action)
	}
	return out, &action, nil
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleContainerAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	in, action, err := parseActionArgs(args(req), containerActions)
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if in.action == containerActionDelete {
		return s.deleteDeployment(ctx, in, func(ctx context.Context) error {
			return client.ContainerDeployments.DeleteDeployment(ctx, in.name, in.timeoutMs)
		})
	}

	apiCtx, cancel := s.apiContext(ctx)
	defer cancel()
	if err := action.exec(apiCtx, client, in.name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return s.actionResult(ctx, in, action, containerStatusReader(client, in.name))
}

// actionResult reports accepted, or completed after wait: true observed the expected status.
func (s *Server) actionResult(ctx context.Context, in *actionArgs, action *deploymentAction, read func(ctx context.Context) (string, error)) (*mcp.CallToolResult, error) {
	result := map[string]any{"name": in.name, "action": in.action, "status": statusAccepted}
	if in.wait && len(action.expectStatuses) > 0 {
		st, err := waitForStatus(ctx, action.expectStatuses, deploymentWaitTimeout, read)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("action %q was accepted but the wait failed: %v", in.action, err)), nil
		}
		result["status"] = statusCompleted
		result["deployment_status"] = st
	}
	return jsonResult(result)
}

// deleteDeployment bounds the call by the server-side wait plus a margin (not
// --timeout), as the CLI does: the default 60 s wait outlives a 30 s budget.
// timeout_ms 0 returns before the deployment is gone, hence accepted.
func (s *Server) deleteDeployment(ctx context.Context, in *actionArgs, del func(ctx context.Context) error) (*mcp.CallToolResult, error) {
	ctx, cancel := context.WithTimeout(ctx, serverless.DeleteTimeout(s.apiTimeout, false, in.timeoutMs))
	defer cancel()
	if err := del(ctx); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	status := statusCompleted
	if in.timeoutMs == 0 {
		status = statusAccepted
	}
	return jsonResult(map[string]any{"name": in.name, "action": in.action, "status": status})
}
