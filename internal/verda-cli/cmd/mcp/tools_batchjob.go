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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
)

// Batch jobs have no spot, no min replicas, no scaling triggers and no
// restart: the invariants in cmd/serverless/CLAUDE.md hold on this surface too.
func (s *Server) registerBatchjobTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_batchjobs",
			mcp.WithDescription("List serverless batch-job deployments (one-shot runs with a per-request deadline). Use describe_batchjob for status and scaling."),
		),
		s.handleListBatchjobs,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("describe_batchjob",
			mcp.WithDescription("Describe a batch-job deployment: image, compute, env var names, scaling (max replicas, request TTL, deadline) and status. The status read is best-effort: a failure lands in status_error."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
		),
		s.handleDescribeBatchjob,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_batchjob",
			mcp.WithDescription("Create a serverless batch-job deployment (bills while jobs run). REQUIRES confirm: true — show the user name/image/compute/deadline first; without it the tool fails with CONFIRMATION_REQUIRED. Required: name (URL slug, immutable), image (specific tag, :latest rejected), compute (from list_serverless_compute), deadline_seconds (600..604800: each queued request gets this long to finish), confirm. Batch jobs cannot use spot compute. Returns the deployment plus its first status read."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name: lowercase alphanumerics and hyphens, max 63 chars")),
			mcp.WithString("image", mcp.Required(), mcp.Description("Container image with a specific tag (:latest is rejected)")),
			mcp.WithString("compute", mcp.Required(), mcp.Description("Compute resource name from list_serverless_compute")),
			mcp.WithNumber("deadline_seconds", mcp.Required(), mcp.Description("Per-request deadline in seconds, 600 (10 min) to 604800 (7 days)")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithNumber("compute_size", mcp.Description("GPUs or vCPU cores per replica (default 1)")),
			mcp.WithNumber("port", mcp.Description("Exposed HTTP port (default 80)")),
			mcp.WithString("registry_credentials", mcp.Description("Registry credentials name from list_registry_credentials; omit for a public image")),
			mcp.WithArray("env", mcp.WithStringItems(), mcp.Description("Plain env vars as KEY=VALUE; use env_secrets for anything sensitive")),
			mcp.WithArray("env_secrets", mcp.WithStringItems(), mcp.Description("Secret-backed env vars as KEY=SECRET_NAME")),
			mcp.WithArray("entrypoint", mcp.WithStringItems(), mcp.Description("Override the image ENTRYPOINT (one element per argument)")),
			mcp.WithArray("cmd", mcp.WithStringItems(), mcp.Description("Override the image CMD (one element per argument)")),
			mcp.WithArray("secret_mounts", mcp.WithStringItems(), mcp.Description("File secrets to mount as SECRET:/absolute/path")),
			mcp.WithNumber("max_replicas", mcp.Description("Maximum worker replicas (default 3)")),
			mcp.WithNumber("request_ttl_seconds", mcp.Description("How long a queued request may wait before it is dropped (default 300)")),
		),
		s.handleCreateBatchjob,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("batchjob_action",
			mcp.WithDescription("Run a lifecycle action on a batch-job deployment: pause, resume, purge_queue or delete. purge_queue and delete are destructive and REQUIRE confirm: true. Returns status \"accepted\" (the API accepted the action) unless wait: true, which for pause polls until the job reports paused and for resume until it reports ready or running (up to 5 min), then returns \"completed\" plus deployment_status; purge_queue is always \"accepted\". delete waits server-side for timeout_ms (default 60 s) and returns \"completed\", or \"accepted\" with timeout_ms 0."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
			mcp.WithString("action", mcp.Required(), mcp.Description("One of: pause, resume, purge_queue, delete")),
			mcp.WithBoolean("confirm", mcp.Description("Set true to confirm destructive actions (purge_queue, delete). Not needed for pause/resume.")),
			mcp.WithBoolean("wait", mcp.Description("pause/resume only: poll until the job reports paused, or ready/running (default false)")),
			mcp.WithNumber("timeout_ms", mcp.Description("delete only: server-side wait in ms, 0 returns immediately, max 300000 (default 60000)")),
		),
		s.handleBatchjobAction,
	)
}

// batchjobView adds the status read to the SDK job, like `verda batchjob describe -o json`.
type batchjobView struct {
	*verda.JobDeployment
	Status      string `json:"status,omitempty"`
	StatusError string `json:"status_error,omitempty"`
}

func batchjobStatusReader(client *verda.Client, name string) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		st, err := client.ServerlessJobs.GetJobDeploymentStatus(ctx, name)
		if err != nil {
			return "", err
		}
		return st.Status, nil
	}
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListBatchjobs(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	jobs, err := client.ServerlessJobs.GetJobDeployments(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(jobs)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleDescribeBatchjob(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	job, err := client.ServerlessJobs.GetJobDeploymentByName(ctx, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := batchjobView{JobDeployment: job}
	view.Status, view.StatusError = readStatus(ctx, batchjobStatusReader(client, name))
	return jsonResult(view)
}

// batchjobCreateOptions maps MCP arguments onto the CLI's create options.
func batchjobCreateOptions(a map[string]any) (*serverless.BatchjobCreateOptions, bool, error) {
	r := &argReader{a: a}
	o := serverless.NewBatchjobCreateOptions()
	o.Name = r.reqStr("name")
	o.Image = r.reqStr("image")
	o.Compute = r.reqStr("compute")
	confirm := r.boolean("confirm")
	r.reqSeconds(&o.Deadline, "deadline_seconds")
	setInt(&o.ComputeSize, r.intPtr("compute_size"))
	setInt(&o.Port, r.intPtr("port"))
	o.RegistryCreds = r.str("registry_credentials")
	o.Env = r.strs("env")
	o.EnvSecret = r.strs("env_secrets")
	o.Entrypoint = r.strs("entrypoint")
	o.Cmd = r.strs("cmd")
	o.SecretMounts = r.strs("secret_mounts")
	setInt(&o.MaxReplicas, r.intPtr("max_replicas"))
	r.seconds(&o.RequestTTL, "request_ttl_seconds")
	return o, confirm, r.err
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleCreateBatchjob(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	opts, confirm, err := batchjobCreateOptions(args(req))
	if err != nil {
		return toolErrorResult(err), nil
	}
	createReq, err := opts.Request()
	if err != nil {
		return toolErrorResult(serverlessArgError(err)), nil
	}
	if !confirm {
		return toolErrorResult(confirmationRequiredError("create_batchjob")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()

	job, err := client.ServerlessJobs.CreateJobDeployment(ctx, createReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	view := batchjobView{JobDeployment: job}
	view.Status, view.StatusError = readStatus(ctx, batchjobStatusReader(client, job.Name))
	return jsonResult(view)
}

// Job statuses from GET /job-deployments/{name}/status (OpenAPI
// GetScaledJobStatusResponseDto: ready, running, paused, terminating). The SDK
// has no constants for them; they differ from the container vocabulary (no
// healthy: an idle job is ready). terminating is not a wait target.
const (
	jobStatusReady   = "ready"   // deployed, idle, waiting for queue messages
	jobStatusRunning = "running" // replicas processing jobs
	jobStatusPaused  = "paused"  // paused via the pause endpoint
)

// batchjobActions mirrors cmd/serverless/batchjob_actions.go: no restart;
// purge-queue is destructive, pause and resume are not. A resumed job is
// ready when idle or running when the queue has work: either ends the wait.
var batchjobActions = map[string]deploymentAction{
	"pause": {
		expectStatuses: []string{jobStatusPaused},
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.PauseJobDeployment(ctx, name)
		},
	},
	"resume": {
		expectStatuses: []string{jobStatusReady, jobStatusRunning},
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.ResumeJobDeployment(ctx, name)
		},
	},
	"purge_queue": {
		destructive: true,
		exec: func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.PurgeJobDeploymentQueue(ctx, name)
		},
	},
	containerActionDelete: {destructive: true},
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleBatchjobAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	in, action, err := parseActionArgs(args(req), batchjobActions)
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if in.action == containerActionDelete {
		return s.deleteDeployment(ctx, in, func(ctx context.Context) error {
			return client.ServerlessJobs.DeleteJobDeployment(ctx, in.name, in.timeoutMs)
		})
	}

	apiCtx, cancel := s.apiContext(ctx)
	defer cancel()
	if err := action.exec(apiCtx, client, in.name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return s.actionResult(ctx, in, action, batchjobStatusReader(client, in.name))
}
