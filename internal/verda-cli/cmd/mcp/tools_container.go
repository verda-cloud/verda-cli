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
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const containerNameParamHint = "Container deployment name (see list_container_deployments)"

// Container tools are registered unconditionally: VERDA_SERVERLESS_ENABLED
// gates only the pre-GA CLI surface, not the public API.
func (s *Server) registerContainerTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_container_deployments",
			mcp.WithDescription("List serverless container deployments: name, endpoint, compute, spot, images. Status is not included (the list endpoint does not return it); call describe_container_deployment for status and scaling."),
		),
		s.handleListContainerDeployments,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("describe_container_deployment",
			mcp.WithDescription("Get one serverless container deployment with its status and scaling options (min/max replicas, triggers, delays). Environment variable values are never returned, only names. If the status or scaling call fails, the deployment is still returned with status_error or scaling_error."),
			mcp.WithString("name", mcp.Required(), mcp.Description(containerNameParamHint)),
		),
		s.handleDescribeContainerDeployment,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_container_replicas",
			mcp.WithDescription("List the current replicas of a serverless container deployment, with a count per status (running, initializing, imagepulling, terminating, error, unavailable). Use it after update_container_scaling to watch replicas start or stop."),
			mcp.WithString("name", mcp.Required(), mcp.Description(containerNameParamHint)),
		),
		s.handleListContainerReplicas,
	)

	s.registerContainerScalingTool()

	s.mcpServer.AddTool(
		mcp.NewTool("container_action",
			mcp.WithDescription("Run a lifecycle action on a serverless container deployment: pause (stops all replicas until resumed), resume, restart (replaces all replicas; in-flight requests are dropped), or purge_queue (drops every pending request; cannot be undone). restart and purge_queue REQUIRE confirm: true — pass it after the user approved the action, or when the user has given standing instructions for this automation; without it the tool fails with CONFIRMATION_REQUIRED and nothing happens. Returns status 'accepted': the action runs asynchronously, so call describe_container_deployment to see the resulting status."),
			mcp.WithString("name", mcp.Required(), mcp.Description(containerNameParamHint)),
			mcp.WithString("action", mcp.Required(), mcp.Description("Action: pause, resume, restart, purge_queue")),
			mcp.WithBoolean("confirm", mcp.Description("Set true to confirm restart or purge_queue. Not needed for pause or resume.")),
		),
		s.handleContainerAction,
	)
}

type containerAction struct {
	destructive bool
	exec        func(s *verda.ContainerDeploymentsService, ctx context.Context, name string) error
}

// containerActions mirrors the CLI action commands (cmd/serverless/container_actions.go):
// restart and purge-queue are destructive there too — keep both in sync.
var containerActions = map[string]containerAction{
	"pause":       {exec: (*verda.ContainerDeploymentsService).PauseDeployment},
	"resume":      {exec: (*verda.ContainerDeploymentsService).ResumeDeployment},
	"restart":     {destructive: true, exec: (*verda.ContainerDeploymentsService).RestartDeployment},
	"purge_queue": {destructive: true, exec: (*verda.ContainerDeploymentsService).PurgeDeploymentQueue},
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleContainerAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := args(req)
	name, err := requiredString(a, "name")
	if err != nil {
		return toolErrorResult(err), nil
	}
	actionName, err := requiredString(a, "action")
	if err != nil {
		return toolErrorResult(err), nil
	}
	action, ok := containerActions[actionName]
	if !ok {
		names := slices.Sorted(maps.Keys(containerActions))
		return toolErrorResult(invalidArgError("action", fmt.Sprintf("invalid value %q (valid: %s)", actionName, strings.Join(names, ", ")))), nil
	}
	confirm, err := optionalBool(a, "confirm")
	if err != nil {
		return toolErrorResult(err), nil
	}
	if action.destructive && !confirm {
		return toolErrorResult(confirmationRequiredError(actionName)), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return toolErrorResult(err), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()
	if err := action.exec(client.ContainerDeployments, ctx, name); err != nil {
		return toolErrorResult(err), nil
	}
	return jsonResult(map[string]any{"name": name, "action": actionName, "status": "accepted"})
}

// containerDeploymentView is the agent-facing deployment shape. It drops env
// var values, which may be secrets — the CLI describe card shows names only.
type containerDeploymentView struct {
	Name            string                  `json:"name"`
	EndpointBaseURL string                  `json:"endpoint_base_url"`
	CreatedAt       time.Time               `json:"created_at,omitzero"`
	Compute         *verda.ContainerCompute `json:"compute,omitempty"`
	IsSpot          bool                    `json:"is_spot"`
	Containers      []containerView         `json:"containers"`
}

type containerView struct {
	Name         string                       `json:"name"`
	Image        string                       `json:"image"`
	ExposedPort  int                          `json:"exposed_port,omitempty"`
	Healthcheck  *verda.ContainerHealthcheck  `json:"healthcheck,omitempty"`
	EnvNames     []string                     `json:"env_names,omitempty"`
	VolumeMounts []verda.ContainerVolumeMount `json:"volume_mounts,omitempty"`
}

// newContainerDeploymentView: detailed adds port, healthcheck, env names and mounts.
func newContainerDeploymentView(d *verda.ContainerDeployment, detailed bool) containerDeploymentView {
	v := containerDeploymentView{
		Name:            d.Name,
		EndpointBaseURL: d.EndpointBaseURL,
		CreatedAt:       d.CreatedAt,
		Compute:         d.Compute,
		IsSpot:          d.IsSpot,
		Containers:      make([]containerView, 0, len(d.Containers)),
	}
	for i := range d.Containers {
		c := &d.Containers[i]
		cv := containerView{Name: c.Name, Image: c.Image.Image}
		if detailed {
			cv.ExposedPort = c.ExposedPort
			cv.Healthcheck = c.Healthcheck
			for _, e := range c.Env {
				cv.EnvNames = append(cv.EnvNames, e.Name)
			}
			cv.VolumeMounts = c.VolumeMounts
		}
		v.Containers = append(v.Containers, cv)
	}
	return v
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListContainerDeployments(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return toolErrorResult(err), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()
	deployments, err := client.ContainerDeployments.GetDeployments(ctx)
	if err != nil {
		return toolErrorResult(err), nil
	}
	out := make([]containerDeploymentView, 0, len(deployments))
	for i := range deployments {
		out = append(out, newContainerDeploymentView(&deployments[i], false))
	}
	return jsonResult(out)
}

type containerDescribeResult struct {
	containerDeploymentView
	Status       string                         `json:"status,omitempty"`
	StatusError  string                         `json:"status_error,omitempty"`
	Scaling      *verda.ContainerScalingOptions `json:"scaling,omitempty"`
	ScalingError string                         `json:"scaling_error,omitempty"`
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleDescribeContainerDeployment(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := requiredString(args(req), "name")
	if err != nil {
		return toolErrorResult(err), nil
	}
	client, err := s.verdaClient()
	if err != nil {
		return toolErrorResult(err), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()
	d, err := client.ContainerDeployments.GetDeploymentByName(ctx, name)
	if err != nil {
		return toolErrorResult(err), nil
	}

	// Status and scaling are best-effort: the deployment itself was found.
	result := containerDescribeResult{containerDeploymentView: newContainerDeploymentView(d, true)}
	if st, err := client.ContainerDeployments.GetDeploymentStatus(ctx, name); err != nil {
		result.StatusError = err.Error()
	} else {
		result.Status = st.Status
	}
	if sc, err := client.ContainerDeployments.GetDeploymentScaling(ctx, name); err != nil {
		result.ScalingError = err.Error()
	} else {
		result.Scaling = sc
	}
	return jsonResult(result)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListContainerReplicas(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := requiredString(args(req), "name")
	if err != nil {
		return toolErrorResult(err), nil
	}
	client, err := s.verdaClient()
	if err != nil {
		return toolErrorResult(err), nil
	}
	ctx, cancel := s.apiContext(ctx)
	defer cancel()
	replicas, err := client.ContainerDeployments.GetDeploymentReplicas(ctx, name)
	if err != nil {
		return toolErrorResult(err), nil
	}

	list := replicas.List
	counts := make(map[string]int)
	for i := range list {
		counts[list[i].Status]++
	}
	return jsonResult(map[string]any{
		"name":             name,
		"count":            len(list),
		"counts_by_status": counts,
		"replicas":         list,
	})
}
