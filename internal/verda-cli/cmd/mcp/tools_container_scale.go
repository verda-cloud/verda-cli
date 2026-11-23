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
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// scaleOptionArgs: a scale call needs at least one of these.
var scaleOptionArgs = []string{
	"min_replicas", "max_replicas", "concurrency", "request_ttl_seconds", "scale_up_delay_seconds",
	"scale_down_delay_seconds", "queue_preset", "queue_load", "cpu_util", "gpu_util",
}

func (s *Server) registerContainerScaleTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("scale_container",
			mcp.WithDescription("Change the scaling options of a container deployment. Only the options you pass are sent; everything else, including triggers you leave out, keeps its current value. REQUIRES confirm: true (raising min_replicas starts billing; mirrors --yes). Returns status \"accepted\" with previous (the options before) and expected (the API's answer); reads lag writes by 5-20 s. With wait: true, polls until reads show the change and returns \"completed\" plus current; a wait failure keeps \"accepted\" and adds wait_error (the change is not rolled back). \"completed\" means the settings show in reads, not that replicas are ready: use list_container_replicas."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Deployment name")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithNumber("min_replicas", mcp.Description("Minimum replicas 0..1000 (0 = scale to zero; above 0 bills with no traffic). Must not exceed the stored maximum.")),
			mcp.WithNumber("max_replicas", mcp.Description("Maximum replicas 1..1000. For no replicas while idle use min_replicas 0 or pause.")),
			mcp.WithNumber("concurrency", mcp.Description("Concurrent requests per replica 1..5000")),
			mcp.WithString("queue_preset", mcp.Description("Queue-load preset: instant (1), balanced (3) or cost-saver (6); or pass queue_load")),
			mcp.WithNumber("queue_load", mcp.Description("Queue-load threshold 1..1000 (fractions allowed)")),
			mcp.WithNumber("cpu_util", mcp.Description("CPU utilization trigger threshold 5..100; 0 turns the trigger off")),
			mcp.WithNumber("gpu_util", mcp.Description("GPU utilization trigger threshold 5..100; 0 turns it off (the API rejects a GPU trigger on CPU compute)")),
			mcp.WithNumber("scale_up_delay_seconds", mcp.Description("Delay before scaling up, 0..3600")),
			mcp.WithNumber("scale_down_delay_seconds", mcp.Description("Delay before scaling down, 0..3600")),
			mcp.WithNumber("request_ttl_seconds", mcp.Description("How long a queued request may wait, 0..86400")),
			mcp.WithBoolean("wait", mcp.Description("Poll until reads show the change (default false)")),
			mcp.WithNumber("wait_timeout_seconds", mcp.Description("Wait budget in seconds (default 60)")),
		),
		s.handleScaleContainer,
	)
}

type scaleArgs struct {
	name        string
	confirm     bool
	wait        bool
	waitTimeout time.Duration
	change      *scaling.Change
}

// parseScaleArgs maps the arguments onto scaling.Change; absent = nil = unchanged.
func parseScaleArgs(a map[string]any) (*scaleArgs, error) {
	r := &argReader{a: a}
	out := &scaleArgs{name: r.reqStr("name"), confirm: r.boolean("confirm"), wait: r.boolean("wait"), waitTimeout: defaultScaleWaitTimeout}
	c := &scaling.Change{
		MinReplicas:           r.intPtr("min_replicas"),
		MaxReplicas:           r.intPtr("max_replicas"),
		Concurrency:           r.intPtr("concurrency"),
		QueueTTLSeconds:       r.intPtr("request_ttl_seconds"),
		ScaleUpDelaySeconds:   r.intPtr("scale_up_delay_seconds"),
		ScaleDownDelaySeconds: r.intPtr("scale_down_delay_seconds"),
		CPUUtil:               r.intPtr("cpu_util"),
		GPUUtil:               r.intPtr("gpu_util"),
		QueueLoad:             r.floatPtr("queue_load"),
	}
	preset := r.enum("queue_preset", serverless.PresetInstant, serverless.PresetBalanced, serverless.PresetCostSaver)
	if wt := r.intPtr("wait_timeout_seconds"); wt != nil {
		out.waitTimeout = time.Duration(*wt) * time.Second
	}
	if r.err != nil {
		return nil, r.err
	}
	if preset != "" && c.QueueLoad == nil {
		n, err := serverless.ResolveQueueLoad(preset, 0)
		if err != nil {
			return nil, invalidArgError("queue_preset", err.Error())
		}
		q := float64(n)
		c.QueueLoad = &q
	}
	if c.Empty() {
		return nil, cmdutil.NewMissingOneOfFlagsError(nil, scaleOptionArgs)
	}
	if out.wait && out.waitTimeout <= 0 {
		return nil, invalidArgError("wait_timeout_seconds", "must be greater than 0")
	}
	out.change = c
	return out, nil
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleScaleContainer(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	in, err := parseScaleArgs(args(req))
	if err != nil {
		return toolErrorResult(err), nil
	}
	patch, err := in.change.Request()
	if err != nil {
		return toolErrorResult(serverlessArgError(err)), nil
	}
	if !in.confirm {
		return toolErrorResult(confirmationRequiredError("scale_container")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	apiCtx, cancel := s.apiContext(ctx)
	defer cancel()

	// The pre-read gives `previous` and the one-bound warning; it is never
	// copied into the PATCH (a lagging read could undo a recent write).
	previous, err := client.ContainerDeployments.GetDeploymentScaling(apiCtx, in.name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	expected, err := client.ContainerDeployments.UpdateDeploymentScaling(apiCtx, in.name, patch)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{"name": in.name, "status": statusAccepted, "previous": previous, "expected": expected}
	if warning := scaling.BoundWarning(previous, patch); warning != "" {
		result["warning"] = warning
	}
	if in.wait {
		s.waitScaling(ctx, client, in, patch, result)
	}
	return jsonResult(result)
}

// waitScaling: completed + current when reads show the change; otherwise the
// result keeps accepted and carries wait_error (the change stands).
func (s *Server) waitScaling(ctx context.Context, client *verda.Client, in *scaleArgs, patch *verda.UpdateScalingOptionsRequest, result map[string]any) {
	current, err := scaling.Wait(ctx, nil, client, in.name, patch, in.waitTimeout)
	if err != nil {
		result["wait_error"] = err.Error()
		return
	}
	result["status"] = statusCompleted
	result["current"] = current
}
