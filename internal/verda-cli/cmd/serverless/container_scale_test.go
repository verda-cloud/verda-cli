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

package serverless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

const testDeployment = "my-api"

// newContainerMock seeds testDeployment: min 0, max 3, CPU trigger on at 70,
// GPU trigger off (threshold 0).
func newContainerMock(t *testing.T) (*mockapi.Server, *verda.Client) {
	t.Helper()
	mock := mockapi.New()
	t.Cleanup(mock.Close)
	mock.SeedContainerDeployment(testDeployment)
	client, err := verda.NewClient(
		verda.WithBaseURL(mock.URL()),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	return mock, client
}

func agentFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, AgentModeOverride: true, OutputFormatOverride: "json"}
}

// runContainer runs `verda container <args>` and returns stdout, stderr.
func runContainer(t *testing.T, f cmdutil.Factory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &out, ErrOut: &errOut})
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// withTerminal makes interactiveTTY see a terminal; not parallel-safe.
func withTerminal(t *testing.T) {
	t.Helper()
	orig := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = orig })
}

func agentErrorCode(err error) string {
	var ae *cmdutil.AgentError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestContainerScale_SendsOnlySetFields(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)

	stdout, _, err := runContainer(t, agentFactory(client), "scale", testDeployment,
		"--min-replicas", "0", "--concurrency", "4", "--scale-down-delay", "2m", "--gpu-util", "off", "--yes")
	if err != nil {
		t.Fatalf("scale: %v", err)
	}

	bodies := mock.ScalingPatchBodies()
	if len(bodies) != 1 {
		t.Fatalf("PATCH count = %d, want 1", len(bodies))
	}
	want := map[string]any{
		"min_replica_count":               float64(0),
		"concurrent_requests_per_replica": float64(4),
		"scale_down_policy":               map[string]any{"delay_seconds": float64(120)},
		"scaling_triggers": map[string]any{
			"gpu_utilization": map[string]any{"enabled": false, "threshold": float64(0)},
		},
	}
	if !reflect.DeepEqual(bodies[0], want) {
		t.Errorf("PATCH body = %v\nwant %v", bodies[0], want)
	}

	var res containerScaleResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if res.Status != scaleStatusAccepted || res.Previous == nil || res.Expected == nil || res.Current != nil {
		t.Errorf("result = %+v, want accepted with previous and expected, no current", res)
	}
	if res.Expected.ConcurrentRequestsPerReplica != 4 || res.Previous.ConcurrentRequestsPerReplica != 1 {
		t.Errorf("expected/previous concurrency = %d/%d, want 4/1",
			res.Expected.ConcurrentRequestsPerReplica, res.Previous.ConcurrentRequestsPerReplica)
	}
}

func TestContainerScale_Triggers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		want    map[string]any // scaling_triggers in the PATCH; nil = no PATCH
		errCode string
	}{
		{
			name: "threshold turns the trigger on",
			args: []string{"--gpu-util", "60"},
			want: map[string]any{"gpu_utilization": map[string]any{"enabled": true, "threshold": float64(60)}},
		},
		{
			name:    "on without a threshold is refused",
			args:    []string{"--cpu-util", "on"},
			errCode: "VALIDATION_ERROR",
		},
		{
			name: "fractional queue load",
			args: []string{"--queue-load", "1.5"},
			want: map[string]any{"queue_load": map[string]any{"threshold": 1.5}},
		},
		{
			name: "named preset",
			args: []string{"--queue-preset", "cost-saver"},
			want: map[string]any{"queue_load": map[string]any{"threshold": float64(6)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock, client := newContainerMock(t)
			args := append([]string{"scale", testDeployment, "--yes"}, tt.args...)
			_, _, err := runContainer(t, agentFactory(client), args...)
			bodies := mock.ScalingPatchBodies()
			if tt.errCode != "" {
				if got := agentErrorCode(err); got != tt.errCode {
					t.Fatalf("error = %v (code %q), want %s", err, got, tt.errCode)
				}
				if len(bodies) != 0 {
					t.Fatalf("PATCH sent on a refused change: %v", bodies)
				}
				return
			}
			if err != nil {
				t.Fatalf("scale: %v", err)
			}
			if len(bodies) != 1 || !reflect.DeepEqual(bodies[0]["scaling_triggers"], tt.want) {
				t.Errorf("PATCH bodies = %v, want scaling_triggers %v", bodies, tt.want)
			}
		})
	}
}

func TestContainerScale_RefusedSendsNoPatch(t *testing.T) {
	t.Run("agent without --yes", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		_, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--min-replicas", "1")
		if got := agentErrorCode(err); got != "CONFIRMATION_REQUIRED" {
			t.Fatalf("error = %v (code %q), want CONFIRMATION_REQUIRED", err, got)
		}
		if n := len(mock.ScalingPatchBodies()); n != 0 {
			t.Errorf("PATCH count = %d, want 0", n)
		}
	})

	t.Run("json output without a terminal needs name, options and --yes", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: "json"}
		for _, args := range [][]string{
			{"scale", testDeployment, "--min-replicas", "1"},
			{"scale", "--min-replicas", "1", "--yes"},
			{"scale", testDeployment, "--yes"},
		} {
			if _, _, err := runContainer(t, f, args...); agentErrorCode(err) == "" {
				t.Errorf("%v: error = %v, want a structured refusal", args, err)
			}
		}
		for _, tc := range []struct {
			args    []string
			missing []string
		}{
			{[]string{"scale"}, []string{"<name>"}},
			{[]string{"scale", testDeployment, "--yes"}, []string{}},
		} {
			_, _, err := runContainer(t, f, tc.args...)
			var ae *cmdutil.AgentError
			if !errors.As(err, &ae) || ae.Code != "MISSING_REQUIRED_FLAGS" {
				t.Fatalf("%v: error = %v, want MISSING_REQUIRED_FLAGS", tc.args, err)
			}
			if !reflect.DeepEqual(ae.Details["missing"], tc.missing) || !reflect.DeepEqual(ae.Details["one_of"], scaleOptionFlags) {
				t.Errorf("%v: details = %v, want missing %v and one_of the option flags", tc.args, ae.Details, tc.missing)
			}
		}
		if n := len(mock.ScalingPatchBodies()); n != 0 {
			t.Errorf("PATCH count = %d, want 0", n)
		}
	})

	t.Run("interactive confirm declined", func(t *testing.T) {
		withTerminal(t)
		mock, client := newContainerMock(t)
		f := &cmdutil.TestFactory{ClientOverride: client, PrompterOverride: tuitest.New().AddConfirm(false)}
		_, stderr, err := runContainer(t, f, "scale", testDeployment, "--min-replicas", "2")
		if err != nil {
			t.Fatalf("scale: %v", err)
		}
		if n := len(mock.ScalingPatchBodies()); n != 0 {
			t.Errorf("PATCH count = %d, want 0", n)
		}
		if !strings.Contains(stderr, "2 replica(s) will run and be billed") {
			t.Errorf("stderr lacks the billing warning:\n%s", stderr)
		}
	})
}

func TestContainerScale_MinAboveMax(t *testing.T) {
	t.Run("both bounds are checked before sending", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		_, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--min-replicas", "5", "--max-replicas", "2", "--yes")
		if got := agentErrorCode(err); got != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "--min-replicas") {
			t.Fatalf("error = %v (code %q), want VALIDATION_ERROR naming --min-replicas", err, got)
		}
		if n := len(mock.ScalingPatchBodies()); n != 0 {
			t.Errorf("PATCH count = %d, want 0", n)
		}
	})

	t.Run("one bound against a stale read is sent", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		mock.SetScalingReadLag(1) // first: the next read must miss the max raise
		if _, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--max-replicas", "10", "--yes"); err != nil {
			t.Fatalf("scale --max-replicas 10: %v", err)
		}
		stdout, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--min-replicas", "5", "--yes")
		if err != nil {
			t.Fatalf("scale --min-replicas 5: %v", err)
		}
		var res containerScaleResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
		}
		if res.Previous.MaxReplicaCount != 3 {
			t.Fatalf("previous max = %d, want the stale 3", res.Previous.MaxReplicaCount)
		}
		bodies := mock.ScalingPatchBodies()
		if want := map[string]any{"min_replica_count": float64(5)}; len(bodies) != 2 || !reflect.DeepEqual(bodies[1], want) {
			t.Errorf("PATCH bodies = %v, want the second to be %v", bodies, want)
		}
	})

	t.Run("one bound above the stored max is an API error", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		_, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--min-replicas", "5", "--yes")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "API_ERROR" || !strings.Contains(err.Error(), "400") {
			t.Fatalf("error = %v, want an API_ERROR 400", err)
		}
		if n := len(mock.ScalingPatchBodies()); n != 1 {
			t.Errorf("PATCH count = %d, want 1 (sent, then rejected)", n)
		}
		sc, err := client.ContainerDeployments.GetDeploymentScaling(context.Background(), testDeployment)
		if err != nil {
			t.Fatalf("GetDeploymentScaling: %v", err)
		}
		if sc.MinReplicaCount != 0 || sc.MaxReplicaCount != 3 {
			t.Errorf("scaling = %d..%d, want unchanged 0..3", sc.MinReplicaCount, sc.MaxReplicaCount)
		}
	})

	t.Run("table output warns on a conflict with the read", func(t *testing.T) {
		withTerminal(t)
		mock, client := newContainerMock(t)
		mock.SetScalingReadLag(1)
		if _, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--max-replicas", "10", "--yes"); err != nil {
			t.Fatalf("scale --max-replicas 10: %v", err)
		}
		f := &cmdutil.TestFactory{ClientOverride: client}
		_, stderr, err := runContainer(t, f, "scale", testDeployment, "--min-replicas", "5", "--yes")
		if err != nil {
			t.Fatalf("scale --min-replicas 5: %v", err)
		}
		if !strings.Contains(stderr, "min replicas 5 is above max replicas 3 in the last read") {
			t.Errorf("stderr lacks the bound warning:\n%s", stderr)
		}
	})
}

func TestContainerScale_Wait(t *testing.T) {
	t.Run("completed once reads show the change", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		mock.SetScalingReadLag(1)
		stdout, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--max-replicas", "7", "--yes", "--wait")
		if err != nil {
			t.Fatalf("scale --wait: %v", err)
		}
		var res containerScaleResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
		}
		if res.Status != scaleStatusCompleted || res.Current == nil || res.Current.MaxReplicaCount != 7 {
			t.Errorf("result = %+v, want completed with current max 7", res)
		}
	})

	t.Run("timeout keeps accepted and fails", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		mock.SetScalingReadLag(1000)
		stdout, _, err := runContainer(t, agentFactory(client), "scale", testDeployment,
			"--max-replicas", "7", "--yes", "--wait", "--wait-timeout", "1s")
		if err == nil || cmdutil.IsPromptCancel(err) {
			t.Fatalf("error = %v, want a non-cancel error", err)
		}
		if !strings.Contains(err.Error(), "was accepted") {
			t.Errorf("error = %q, want it to say the change was accepted", err)
		}
		var res containerScaleResult
		dec := json.NewDecoder(strings.NewReader(stdout))
		if err := dec.Decode(&res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if dec.More() {
			t.Errorf("stdout has more than one JSON document:\n%s", stdout)
		}
		if res.Status != scaleStatusAccepted || res.WaitError == "" || res.Current != nil {
			t.Errorf("result = %+v, want accepted with wait_error and no current", res)
		}
		if n := len(mock.ScalingPatchBodies()); n != 1 {
			t.Errorf("PATCH count = %d, want 1", n)
		}
	})
}

func TestBuildScaleFlow(t *testing.T) {
	t.Parallel()
	cur := &verda.ContainerScalingOptions{
		MinReplicaCount: 0, MaxReplicaCount: 3,
		ScalingTriggers: &verda.ScalingTriggers{
			CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
			GPUUtilization: &verda.UtilizationTrigger{},
		},
	}

	t.Run("collects only the picked settings", func(t *testing.T) {
		t.Parallel()
		change := &scaling.Change{}
		engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
			wizard.MultiSelectResult([]int{0, 1, 5}), // min, max, GPU trigger
			wizard.TextResult("4"),                   // min
			wizard.TextResult("8"),                   // max: both raised, min > old max
			wizard.SelectResult(1),                   // GPU: on
			wizard.TextResult("60"),                  // GPU threshold
		))
		if err := engine.Run(context.Background(), buildScaleFlow(cur, change)); err != nil {
			t.Fatalf("engine Run: %v", err)
		}
		req, err := change.Request()
		if err != nil {
			t.Fatalf("Request: %v", err)
		}
		if *req.MinReplicaCount != 4 || *req.MaxReplicaCount != 8 || req.ConcurrentRequestsPerReplica != nil {
			t.Errorf("request replicas = %v/%v concurrency %v", req.MinReplicaCount, req.MaxReplicaCount, req.ConcurrentRequestsPerReplica)
		}
		if g := req.ScalingTriggers.GPUUtilization; g == nil || !g.Enabled || g.Threshold != 60 || req.ScalingTriggers.CPUUtilization != nil {
			t.Errorf("triggers = %+v, want only GPU on at 60", req.ScalingTriggers)
		}
	})

	t.Run("deselecting after back drops the change", func(t *testing.T) {
		t.Parallel()
		change := &scaling.Change{}
		engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
			wizard.MultiSelectResult([]int{0, 2}), // min, concurrency
			wizard.TextResult("2"),                // min
			wizard.BackResult(),                   // back from concurrency to min
			wizard.BackResult(),                   // back to settings
			wizard.MultiSelectResult([]int{2}),    // concurrency only
			wizard.TextResult("6"),                // concurrency
		))
		if err := engine.Run(context.Background(), buildScaleFlow(cur, change)); err != nil {
			t.Fatalf("engine Run: %v", err)
		}
		if change.MinReplicas != nil {
			t.Errorf("MinReplicas = %d, want nil after deselecting it", *change.MinReplicas)
		}
		if change.Concurrency == nil || *change.Concurrency != 6 {
			t.Errorf("Concurrency = %v, want 6", change.Concurrency)
		}
	})

	t.Run("picked settings equal to the read are sent", func(t *testing.T) {
		t.Parallel()
		change, err := runScaleWizard(context.Background(), &cmdutil.TestFactory{}, cmdutil.IOStreams{Out: io.Discard, ErrOut: io.Discard}, cur,
			wizard.WithTestResults(
				wizard.MultiSelectResult([]int{0, 2}), // min, concurrency
				wizard.TextResult("0"),                // min: the prefilled read value
				wizard.TextResult("6"),                // concurrency
			))
		if err != nil {
			t.Fatalf("runScaleWizard: %v", err)
		}
		if change == nil || change.MinReplicas == nil || *change.MinReplicas != 0 {
			t.Fatalf("change = %+v, want MinReplicas 0 kept", change)
		}
		if change.Concurrency == nil || *change.Concurrency != 6 {
			t.Errorf("Concurrency = %v, want 6", change.Concurrency)
		}
	})
}

func TestRenderScalingDiff_MarksValuesEqualToRead(t *testing.T) {
	t.Parallel()
	cur := &verda.ContainerScalingOptions{MinReplicaCount: 0, MaxReplicaCount: 3}
	var buf bytes.Buffer
	renderScalingDiff(&buf, testDeployment, cur, &verda.UpdateScalingOptionsRequest{MinReplicaCount: new(0), MaxReplicaCount: new(5)})
	var minLine, maxLine string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		switch {
		case strings.Contains(line, "Min replicas"):
			minLine = line
		case strings.Contains(line, "Max replicas"):
			maxLine = line
		}
	}
	if !strings.Contains(minLine, "(same as last read") {
		t.Errorf("min row = %q, want the same-as-read note", minLine)
	}
	if strings.Contains(maxLine, "(same as last read") {
		t.Errorf("max row = %q, want no note: the value changes", maxLine)
	}
}

// A wizard pick equal to a lagging read is still sent: it may revert a write the read doesn't show yet.
func TestRunScaleWizard_SendsPicksEqualToStaleRead(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	ctx := context.Background()
	if _, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--cpu-util", "off", "--yes"); err != nil {
		t.Fatalf("scale --cpu-util off: %v", err)
	}
	mock.SetScalingReadLag(1) // before the PATCH: the stale snapshot is taken then
	if _, _, err := runContainer(t, agentFactory(client), "scale", testDeployment, "--cpu-util", "70", "--yes"); err != nil {
		t.Fatalf("scale --cpu-util 70: %v", err)
	}
	stale, err := client.ContainerDeployments.GetDeploymentScaling(ctx, testDeployment)
	if err != nil {
		t.Fatalf("GetDeploymentScaling: %v", err)
	}
	if c := stale.ScalingTriggers.CPUUtilization; c != nil && c.Enabled {
		t.Fatalf("read shows CPU %+v, want the stale off", c)
	}

	f := &cmdutil.TestFactory{ClientOverride: client}
	change, err := runScaleWizard(ctx, f, cmdutil.IOStreams{Out: io.Discard, ErrOut: io.Discard}, stale, wizard.WithTestResults(
		wizard.MultiSelectResult([]int{4}), // CPU trigger
		wizard.SelectResult(0),             // Off
	))
	if err != nil {
		t.Fatalf("runScaleWizard: %v", err)
	}
	if change == nil || change.CPUUtil == nil || *change.CPUUtil != 0 {
		t.Fatalf("change = %+v, want CPU off", change)
	}
	req, err := change.Request()
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if c := req.ScalingTriggers.CPUUtilization; c == nil || c.Enabled {
		t.Errorf("request CPU trigger = %+v, want {enabled:false}", c)
	}
}

func TestWholeSeconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		d       time.Duration
		maxD    time.Duration
		want    int
		wantErr string
	}{
		{"30s", 30 * time.Second, 10 * time.Minute, 30, ""},
		{"0", 0, 10 * time.Minute, 0, ""},
		{"max boundary", 10 * time.Minute, 10 * time.Minute, 600, ""},
		{"fractional", 1500 * time.Millisecond, 10 * time.Minute, 0, "whole seconds"},
		{"negative", -1 * time.Second, 10 * time.Minute, 0, "must be between 0s"},
		{"over max", 11 * time.Minute, 10 * time.Minute, 0, "must be between 0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := wholeSeconds(tt.d, tt.maxD)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("wholeSeconds(%s, %s) error = %v, want %q", tt.d, tt.maxD, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("wholeSeconds(%s, %s) unexpected error: %v", tt.d, tt.maxD, err)
			}
			if got != tt.want {
				t.Errorf("wholeSeconds(%s, %s) = %d, want %d", tt.d, tt.maxD, got, tt.want)
			}
		})
	}
}

func TestScalingDiff_WithTriggers(t *testing.T) {
	t.Parallel()
	cur := &verda.ContainerScalingOptions{
		MinReplicaCount:              0,
		MaxReplicaCount:              3,
		ConcurrentRequestsPerReplica: 1,
		ScalingTriggers: &verda.ScalingTriggers{
			QueueLoad:      &verda.QueueLoadTrigger{Threshold: 3},
			CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
			GPUUtilization: &verda.UtilizationTrigger{Enabled: false, Threshold: 0},
		},
	}
	req := &verda.UpdateScalingOptionsRequest{
		ScalingTriggers: &verda.ScalingTriggers{
			QueueLoad:      &verda.QueueLoadTrigger{Threshold: 6},
			CPUUtilization: &verda.UtilizationTrigger{Enabled: false, Threshold: 0},
			GPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 80},
		},
	}
	rows := scalingDiff(cur, req)
	labels := make(map[string]bool)
	for _, r := range rows {
		labels[r.label] = true
	}
	for _, want := range []string{"Queue load", "CPU trigger", "GPU trigger"} {
		if !labels[want] {
			t.Errorf("expected row %q in diff, got labels: %v", want, labels)
		}
	}
}

func TestScalingDiff_ReplicasAndConcurrency(t *testing.T) {
	t.Parallel()
	cur := &verda.ContainerScalingOptions{
		MinReplicaCount:              0,
		MaxReplicaCount:              3,
		ConcurrentRequestsPerReplica: 1,
	}
	req := &verda.UpdateScalingOptionsRequest{
		MinReplicaCount:              new(2),
		MaxReplicaCount:              new(10),
		ConcurrentRequestsPerReplica: new(4),
	}
	rows := scalingDiff(cur, req)
	labels := make(map[string]bool)
	for _, r := range rows {
		labels[r.label] = true
	}
	for _, want := range []string{"Min replicas", "Max replicas", "Concurrency"} {
		if !labels[want] {
			t.Errorf("expected row %q in diff", want)
		}
	}
}

func TestRenderScaleResult(t *testing.T) {
	t.Parallel()
	t.Run("completed", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		renderScaleResult(&buf, &containerScaleResult{Name: "my-api", Status: scaleStatusCompleted})
		if !strings.Contains(buf.String(), "completed") {
			t.Errorf("expected 'completed' in output; got:\n%s", buf.String())
		}
	})
	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		renderScaleResult(&buf, &containerScaleResult{Name: "my-api", Status: scaleStatusAccepted})
		if !strings.Contains(buf.String(), "accepted") {
			t.Errorf("expected 'accepted' in output; got:\n%s", buf.String())
		}
	})
}

func TestParseUtilFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"number", "50", 50, false},
		{"off string", "off", 0, false},
		{"OFF caps", "OFF", 0, false},
		{"invalid", "abc", 0, true},
		{"float", "3.5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseUtilFlag("--cpu-util", tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *got != tt.want {
				t.Errorf("parseUtilFlag(%q) = %d, want %d", tt.input, *got, tt.want)
			}
		})
	}
}

func TestRenderScalingCard(t *testing.T) {
	t.Parallel()
	sc := &verda.ContainerScalingOptions{
		MinReplicaCount:              0,
		MaxReplicaCount:              5,
		ConcurrentRequestsPerReplica: 2,
		ScalingTriggers: &verda.ScalingTriggers{
			QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
		},
	}
	var buf bytes.Buffer
	renderScalingCard(&buf, "my-api", sc)
	out := buf.String()
	for _, want := range []string{"Current scaling for my-api", "0..5", "Concurrency", "Queue load"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output; got:\n%s", want, out)
		}
	}
}

func TestContainerScale_TableOutput(t *testing.T) {
	withTerminal(t)
	_, client := newContainerMock(t)
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}

	stdout, stderr, err := runContainer(t, f, "scale", testDeployment, "--max-replicas", "5", "--yes")
	if err != nil {
		t.Fatalf("scale: %v", err)
	}
	if !strings.Contains(stderr, "Max replicas") {
		t.Errorf("stderr lacks scaling diff:\n%s", stderr)
	}
	if !strings.Contains(stdout, "accepted") || !strings.Contains(stdout, "replicas") {
		t.Errorf("stdout lacks result message:\n%s", stdout)
	}
}

func TestFlagError(t *testing.T) {
	t.Parallel()
	fe := &scaling.FieldError{Field: "min-replicas", Message: "must be >= 0"}
	wrapped := fmt.Errorf("validation: %w", fe)
	got := flagError(wrapped)
	if got == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Error(), "--min-replicas") {
		t.Errorf("expected --min-replicas in error; got: %v", got)
	}

	plain := errors.New("some other error")
	if !errors.Is(flagError(plain), plain) {
		t.Error("expected plain error to pass through")
	}
}

// A read that fails for good (deleted deployment, bad credentials) must end
// --wait at once with the API error, not after the timeout.
func TestScalingWait_StopsOnClientError(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	mock.FailRoute("/container-deployments/"+testDeployment+"/scaling", http.StatusNotFound)

	start := time.Now()
	_, err := scaling.Wait(context.Background(), nil, client, testDeployment,
		&verda.UpdateScalingOptionsRequest{MaxReplicaCount: new(7)}, 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v, want the 404", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Wait took %s, want it to stop at the first 404", d)
	}
}
