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
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	argMinReplicas   = "min_replica_count"
	argMaxReplicas   = "max_replica_count"
	minUtilThreshold = 5
)

// Scaling changes take 10-20 s to show in reads.
const (
	scalingWaitTimeout  = 60 * time.Second
	scalingWaitInterval = 2 * time.Second
)

// statusApplied: wait: true observed the change in reads.
const statusApplied = "applied"

// scaleToZeroHint: the spec requires max >= 1; zero replicas come from min 0 or pause.
const scaleToZeroHint = "; to run no replicas while idle set min_replica_count: 0, or pause the deployment with container_action"

const updateScalingDescription = "Change the scaling options of a serverless container deployment. Only the fields you pass change; everything else, including scaling triggers you leave out, keeps its current value. " +
	"Raising min_replica_count keeps that many replicas running and billed even with no traffic; max_replica_count caps billed replicas under load. " +
	"REQUIRES confirm: true. Pass it after the user approved this exact change, or when the user has given standing instructions for this automation (for example: scale to 0 at night). Without it the tool fails with CONFIRMATION_REQUIRED and changes nothing. " +
	"Returns status 'accepted' with the previous options (may lag an earlier update) and the expected options (the API's answer). Reads show a change after 10-20 seconds; pass wait: true to poll until they do and get status 'applied' with the current options. " +
	"Replicas then start or stop asynchronously; call list_container_replicas to see them."

func (s *Server) registerContainerScalingTool() {
	s.mcpServer.AddTool(
		mcp.NewTool("update_container_scaling",
			mcp.WithDescription(updateScalingDescription),
			mcp.WithString("name", mcp.Required(), mcp.Description(containerNameParamHint)),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithNumber(argMinReplicas, mcp.Description("Minimum replicas, 0-1000. 0 lets the deployment scale to zero when idle.")),
			mcp.WithNumber(argMaxReplicas, mcp.Description("Maximum replicas, 1-1000.")),
			mcp.WithNumber("concurrent_requests_per_replica", mcp.Description("Requests each replica handles at once, 1-5000.")),
			mcp.WithNumber("queue_message_ttl_seconds", mcp.Description("Seconds before a queued request is dropped, 0-86400.")),
			mcp.WithNumber("scale_up_delay_seconds", mcp.Description("Cooldown before scaling up, 0-3600.")),
			mcp.WithNumber("scale_down_delay_seconds", mcp.Description("Cooldown before scaling down, 0-3600.")),
			mcp.WithNumber("queue_load_threshold", mcp.Description("Queued requests per replica that trigger a scale-up, 1-1000; fractions allowed.")),
			mcp.WithBoolean("cpu_utilization_enabled", mcp.Description("Turn the CPU utilization trigger on or off.")),
			mcp.WithNumber("cpu_utilization_threshold", mcp.Description("CPU utilization percent that triggers a scale-up, 5-100. Setting it turns the trigger on unless cpu_utilization_enabled is false.")),
			mcp.WithBoolean("gpu_utilization_enabled", mcp.Description("Turn the GPU utilization trigger on or off.")),
			mcp.WithNumber("gpu_utilization_threshold", mcp.Description("GPU utilization percent that triggers a scale-up, 5-100. Setting it turns the trigger on unless gpu_utilization_enabled is false. Not supported on CPU compute: the API rejects the update.")),
			mcp.WithBoolean("wait", mcp.Description("Poll until the change is visible in reads, up to 60 seconds (default false: return 'accepted' immediately)")),
		),
		s.handleUpdateContainerScaling,
	)
}

// utilArgs: a nil enabled with a set threshold means "turn on".
type utilArgs struct {
	enabled   *bool
	threshold *int
}

func (u utilArgs) set() bool { return u.enabled != nil || u.threshold != nil }

type scalingArgs struct {
	minReplicas, maxReplicas *int
	concurrency, ttl         *int
	upDelay, downDelay       *int
	queueLoad                *float64
	cpu, gpu                 utilArgs
}

func (sa *scalingArgs) empty() bool {
	return sa.minReplicas == nil && sa.maxReplicas == nil && sa.concurrency == nil && sa.ttl == nil &&
		sa.upDelay == nil && sa.downDelay == nil && sa.queueLoad == nil && !sa.cpu.set() && !sa.gpu.set()
}

var scalingArgNames = []string{
	argMinReplicas, argMaxReplicas, "concurrent_requests_per_replica", "queue_message_ttl_seconds",
	"scale_up_delay_seconds", "scale_down_delay_seconds", "queue_load_threshold",
	"cpu_utilization_enabled", "cpu_utilization_threshold", "gpu_utilization_enabled", "gpu_utilization_threshold",
}

func parseScalingArgs(a map[string]any) (*scalingArgs, error) {
	sa := &scalingArgs{}
	ints := []struct {
		name   string
		dst    **int
		lo, hi int
		hint   string
	}{
		// Limits from PatchScalingOptionsPublicApiDto in the public OpenAPI spec.
		{argMinReplicas, &sa.minReplicas, 0, 1000, ""},
		{argMaxReplicas, &sa.maxReplicas, 1, 1000, scaleToZeroHint},
		{"concurrent_requests_per_replica", &sa.concurrency, 1, 5000, ""},
		{"queue_message_ttl_seconds", &sa.ttl, 0, 86400, ""},
		{"scale_up_delay_seconds", &sa.upDelay, 0, 3600, ""},
		{"scale_down_delay_seconds", &sa.downDelay, 0, 3600, ""},
		{"cpu_utilization_threshold", &sa.cpu.threshold, minUtilThreshold, 100, ""},
		{"gpu_utilization_threshold", &sa.gpu.threshold, minUtilThreshold, 100, ""},
	}
	for _, f := range ints {
		v, err := optionalIntPtr(a, f.name)
		if err != nil {
			return nil, err
		}
		if v != nil && (*v < f.lo || *v > f.hi) {
			return nil, invalidArgError(f.name, fmt.Sprintf("must be between %d and %d%s", f.lo, f.hi, f.hint))
		}
		*f.dst = v
	}

	var err error
	if sa.queueLoad, err = optionalFloatPtr(a, "queue_load_threshold"); err != nil {
		return nil, err
	}
	if q := sa.queueLoad; q != nil && (*q < 1 || *q > 1000) {
		return nil, invalidArgError("queue_load_threshold", "must be between 1 and 1000")
	}
	if sa.cpu.enabled, err = optionalBoolPtr(a, "cpu_utilization_enabled"); err != nil {
		return nil, err
	}
	if sa.gpu.enabled, err = optionalBoolPtr(a, "gpu_utilization_enabled"); err != nil {
		return nil, err
	}

	if sa.empty() {
		return nil, &cmdutil.AgentError{
			Code:     "MISSING_REQUIRED_FLAGS",
			Message:  "set at least one scaling option",
			Details:  map[string]any{"missing": scalingArgNames},
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	return sa, nil
}

// request builds the PATCH body against the current options.
func (sa *scalingArgs) request(cur *verda.ContainerScalingOptions) (*verda.UpdateScalingOptionsRequest, error) {
	effMin, effMax := cur.MinReplicaCount, cur.MaxReplicaCount
	if sa.minReplicas != nil {
		effMin = *sa.minReplicas
	}
	if sa.maxReplicas != nil {
		effMax = *sa.maxReplicas
	}
	if effMin > effMax {
		return nil, invalidArgError(argMinReplicas, fmt.Sprintf("min_replica_count (%d) must not be greater than max_replica_count (%d)", effMin, effMax))
	}

	var curTriggers verda.ScalingTriggers
	if cur.ScalingTriggers != nil {
		curTriggers = *cur.ScalingTriggers
	}
	cpu, err := sa.cpu.trigger("cpu_utilization", curTriggers.CPUUtilization)
	if err != nil {
		return nil, err
	}
	gpu, err := sa.gpu.trigger("gpu_utilization", curTriggers.GPUUtilization)
	if err != nil {
		return nil, err
	}

	req := verda.UpdateScalingOptionsRequest{
		MinReplicaCount:              sa.minReplicas,
		MaxReplicaCount:              sa.maxReplicas,
		QueueMessageTTLSeconds:       sa.ttl,
		ConcurrentRequestsPerReplica: sa.concurrency,
	}
	if sa.upDelay != nil {
		req.ScaleUpPolicy = &verda.ScalingPolicy{DelaySeconds: *sa.upDelay}
	}
	if sa.downDelay != nil {
		req.ScaleDownPolicy = &verda.ScalingPolicy{DelaySeconds: *sa.downDelay}
	}
	// Omitted triggers keep their values server-side; if that regresses, fill
	// them from cur here instead of leaving them nil.
	if sa.queueLoad != nil || cpu != nil || gpu != nil {
		req.ScalingTriggers = &verda.ScalingTriggers{CPUUtilization: cpu, GPUUtilization: gpu}
		if sa.queueLoad != nil {
			req.ScalingTriggers.QueueLoad = &verda.QueueLoadTrigger{Threshold: *sa.queueLoad}
		}
	}
	return &req, nil
}

// trigger resolves a util trigger change; nil means "leave it out".
func (u utilArgs) trigger(prefix string, cur *verda.UtilizationTrigger) (*verda.UtilizationTrigger, error) {
	if !u.set() {
		return nil, nil
	}
	// Always send a disable: reads lag, so "already off" may be stale.
	if u.enabled != nil && !*u.enabled {
		return &verda.UtilizationTrigger{}, nil
	}

	threshold := 0
	if cur != nil {
		threshold = cur.Threshold
	}
	if u.threshold != nil {
		threshold = *u.threshold
	}
	if threshold < minUtilThreshold {
		return nil, invalidArgError(prefix+"_threshold", "required to turn the trigger on: it has no current threshold (5-100)")
	}
	return &verda.UtilizationTrigger{Enabled: true, Threshold: threshold}, nil
}

// scalingApplied reports whether got shows every field req sets. Fields req
// leaves out are not compared: the pre-read they would come from may itself
// lag an earlier update.
func scalingApplied(got *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) bool {
	ints := []struct {
		want *int
		got  int
	}{
		{req.MinReplicaCount, got.MinReplicaCount},
		{req.MaxReplicaCount, got.MaxReplicaCount},
		{req.QueueMessageTTLSeconds, got.QueueMessageTTLSeconds},
		{req.ConcurrentRequestsPerReplica, got.ConcurrentRequestsPerReplica},
	}
	for _, f := range ints {
		if f.want != nil && *f.want != f.got {
			return false
		}
	}
	if !policyApplied(req.ScaleUpPolicy, got.ScaleUpPolicy) || !policyApplied(req.ScaleDownPolicy, got.ScaleDownPolicy) {
		return false
	}
	t := req.ScalingTriggers
	if t == nil {
		return true
	}
	var gt verda.ScalingTriggers
	if got.ScalingTriggers != nil {
		gt = *got.ScalingTriggers
	}
	if t.QueueLoad != nil && (gt.QueueLoad == nil || *gt.QueueLoad != *t.QueueLoad) {
		return false
	}
	return utilApplied(t.CPUUtilization, gt.CPUUtilization) && utilApplied(t.GPUUtilization, gt.GPUUtilization)
}

func policyApplied(want, got *verda.ScalingPolicy) bool {
	return want == nil || (got != nil && *got == *want)
}

// utilApplied: a disabled trigger reads back with threshold 0, so only enabled is compared.
func utilApplied(want, got *verda.UtilizationTrigger) bool {
	switch {
	case want == nil:
		return true
	case !want.Enabled:
		return got == nil || !got.Enabled
	default:
		return got != nil && *got == *want
	}
}

// waitScalingApplied polls reads until they show every field req sets; another
// writer changing those fields meanwhile also ends in a timeout.
func waitScalingApplied(ctx context.Context, client *verda.Client, name string, req *verda.UpdateScalingOptionsRequest) (*verda.ContainerScalingOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, scalingWaitTimeout)
	defer cancel()
	var lastErr error
	for {
		got, err := client.ContainerDeployments.GetDeploymentScaling(ctx, name)
		if err == nil && scalingApplied(got, req) {
			return got, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("change not visible after %s: %w", scalingWaitTimeout, lastErr)
			}
			return nil, fmt.Errorf("change not visible after %s", scalingWaitTimeout)
		case <-time.After(scalingWaitInterval):
		}
	}
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleUpdateContainerScaling(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := args(req)
	name, err := requiredString(a, "name")
	if err != nil {
		return toolErrorResult(err), nil
	}
	sa, err := parseScalingArgs(a)
	if err != nil {
		return toolErrorResult(err), nil
	}
	wait, err := optionalBool(a, "wait")
	if err != nil {
		return toolErrorResult(err), nil
	}
	confirm, err := optionalBool(a, "confirm")
	if err != nil {
		return toolErrorResult(err), nil
	}
	// Billing action (min replicas run while idle): gated before any API call.
	if !confirm {
		return toolErrorResult(confirmationRequiredError("update_container_scaling")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return toolErrorResult(err), nil
	}
	apiCtx, cancel := s.apiContext(ctx)
	defer cancel()
	cur, err := client.ContainerDeployments.GetDeploymentScaling(apiCtx, name)
	if err != nil {
		return toolErrorResult(err), nil
	}
	upd, err := sa.request(cur)
	if err != nil {
		return toolErrorResult(err), nil
	}
	// The PATCH body is the stored options with this change, not yet what reads
	// return: never report it as current.
	expected, err := client.ContainerDeployments.UpdateDeploymentScaling(apiCtx, name, upd)
	if err != nil {
		return toolErrorResult(err), nil
	}

	result := map[string]any{
		"name":     name,
		"status":   "accepted",
		"previous": cur,
		"expected": expected,
	}
	if wait {
		if got, err := waitScalingApplied(ctx, client, name, upd); err != nil {
			result["wait_error"] = err.Error()
		} else {
			result["status"] = statusApplied
			result["current"] = got
		}
	}
	return jsonResult(result)
}
