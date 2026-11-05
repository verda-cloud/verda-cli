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
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless/scaling"
)

// Setting keys of the scale wizard's "settings" multi-select.
const (
	scaleSetMin            = "min-replicas"
	scaleSetMax            = "max-replicas"
	scaleSetConcurrency    = "concurrency"
	scaleSetQueueLoad      = "queue-load"
	scaleSetCPU            = "cpu-util"
	scaleSetGPU            = "gpu-util"
	scaleSetScaleUpDelay   = "scale-up-delay"
	scaleSetScaleDownDelay = "scale-down-delay"
	scaleSetRequestTTL     = "request-ttl"

	scaleStepSettings = "settings"

	utilOn = "on"
)

// buildScaleFlow edits change in place; a deselected setting's step resets its pointer, dropping it from the PATCH.
func buildScaleFlow(cur *verda.ContainerScalingOptions, change *scaling.Change) *wizard.Flow {
	t := scaling.Triggers(cur)
	return &wizard.Flow{
		Name: "container-scale",
		Steps: []wizard.Step{
			scaleStepPickSettings(cur, &t),
			scaleIntStep(scaleSetMin, "Min replicas (0 = scale to zero when idle)", cur.MinReplicaCount, 0, maxReplicas, parseNonNegativeIntValidator("min replicas", maxReplicas), &change.MinReplicas),
			scaleIntStep(scaleSetMax, "Max replicas", cur.MaxReplicaCount, 1, maxReplicas, parsePositiveIntValidator("max replicas", maxReplicas), &change.MaxReplicas),
			scaleIntStep(scaleSetConcurrency, "Concurrent requests per replica", cur.ConcurrentRequestsPerReplica, 1, maxConcurrency, parsePositiveIntValidator("concurrency", maxConcurrency), &change.Concurrency),
			scaleQueuePresetStep(&change.QueueLoad),
			scaleQueueCustomStep(t.QueueLoad, &change.QueueLoad),
			scaleUtilModeStep(scaleSetCPU, "CPU utilization trigger", &change.CPUUtil),
			scaleUtilThresholdStep(scaleSetCPU, "CPU", t.CPUUtilization, &change.CPUUtil),
			scaleUtilModeStep(scaleSetGPU, "GPU utilization trigger", &change.GPUUtil),
			scaleUtilThresholdStep(scaleSetGPU, "GPU", t.GPUUtilization, &change.GPUUtil),
			scaleSecondsStep(scaleSetScaleUpDelay, "Scale-up delay", policySeconds(cur.ScaleUpPolicy), maxScalingDelay, &change.ScaleUpDelaySeconds),
			scaleSecondsStep(scaleSetScaleDownDelay, "Scale-down delay", policySeconds(cur.ScaleDownPolicy), maxScalingDelay, &change.ScaleDownDelaySeconds),
			scaleSecondsStep(scaleSetRequestTTL, "Request TTL", cur.QueueMessageTTLSeconds, maxContainerRequestTTL, &change.QueueTTLSeconds),
		},
	}
}

func scaleStepPickSettings(cur *verda.ContainerScalingOptions, t *verda.ScalingTriggers) wizard.Step {
	return wizard.Step{
		Name:        scaleStepSettings,
		Description: "Settings to change",
		Prompt:      wizard.MultiSelectPrompt,
		Required:    true,
		MinError:    func(int) string { return "pick at least one setting (esc to cancel)" },
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: fmt.Sprintf("Min replicas (%d)", cur.MinReplicaCount), Value: scaleSetMin},
			wizard.Choice{Label: fmt.Sprintf("Max replicas (%d)", cur.MaxReplicaCount), Value: scaleSetMax},
			wizard.Choice{Label: fmt.Sprintf("Concurrency (%d)", cur.ConcurrentRequestsPerReplica), Value: scaleSetConcurrency},
			wizard.Choice{Label: fmt.Sprintf("Queue load (%s)", queueLoadLabel(t.QueueLoad)), Value: scaleSetQueueLoad},
			wizard.Choice{Label: fmt.Sprintf("CPU trigger (%s)", utilLabel(t.CPUUtilization)), Value: scaleSetCPU},
			wizard.Choice{Label: fmt.Sprintf("GPU trigger (%s)", utilLabel(t.GPUUtilization)), Value: scaleSetGPU},
			wizard.Choice{Label: fmt.Sprintf("Scale-up delay (%s)", fmtSeconds(policySeconds(cur.ScaleUpPolicy))), Value: scaleSetScaleUpDelay},
			wizard.Choice{Label: fmt.Sprintf("Scale-down delay (%s)", fmtSeconds(policySeconds(cur.ScaleDownPolicy))), Value: scaleSetScaleDownDelay},
			wizard.Choice{Label: fmt.Sprintf("Request TTL (%s)", fmtSeconds(cur.QueueMessageTTLSeconds)), Value: scaleSetRequestTTL},
		),
		Setter:   func(any) {},
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

func scaleSelected(c map[string]any, key string) bool {
	picked, _ := c[scaleStepSettings].([]string)
	return slices.Contains(picked, key)
}

func scaleIntStep(key, desc string, current, lo, hi int, validate func(any) error, dst **int) wizard.Step {
	return wizard.Step{
		Name:        key,
		Description: fmt.Sprintf("%s (%d..%d)", desc, lo, hi),
		Prompt:      wizard.TextInputPrompt,
		DependsOn:   []string{scaleStepSettings},
		ShouldSkip:  func(c map[string]any) bool { return !scaleSelected(c, key) },
		Default:     func(map[string]any) any { return strconv.Itoa(current) },
		Validate:    validate,
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*dst = &n
		},
		Resetter: func() { *dst = nil },
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

func scaleSecondsStep(key, desc string, current int, maxD time.Duration, dst **int) wizard.Step {
	return wizard.Step{
		Name:        key,
		Description: fmt.Sprintf("%s (0s..%s, e.g. 30s, 5m)", desc, shortDuration(maxD)),
		Prompt:      wizard.TextInputPrompt,
		DependsOn:   []string{scaleStepSettings},
		ShouldSkip:  func(c map[string]any) bool { return !scaleSelected(c, key) },
		Default:     func(map[string]any) any { return fmtSeconds(current) },
		Validate: func(v any) error {
			d, err := time.ParseDuration(strings.TrimSpace(v.(string)))
			if err != nil {
				return errors.New("must be a duration (e.g. 0s, 30s, 5m)")
			}
			_, err = wholeSeconds(d, maxD)
			return err
		},
		Setter: func(v any) {
			d, _ := time.ParseDuration(strings.TrimSpace(v.(string)))
			secs, _ := wholeSeconds(d, maxD)
			*dst = &secs
		},
		Resetter: func() { *dst = nil },
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

// scaleQueuePresetStep owns change.QueueLoad's reset; the custom step's
// Resetter is a no-op so skipping it (named preset) can't clobber the value.
func scaleQueuePresetStep(dst **float64) wizard.Step {
	return wizard.Step{
		Name:        scaleSetQueueLoad,
		Description: "Queue-load preset",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{scaleStepSettings},
		ShouldSkip:  func(c map[string]any) bool { return !scaleSelected(c, scaleSetQueueLoad) },
		Loader:      wizard.StaticChoices(queuePresetChoices(true, "Set a threshold yourself (fractions allowed).")...),
		Setter: func(v any) {
			if n, err := resolveQueueLoad(v.(string), 0); err == nil {
				q := float64(n)
				*dst = &q
			}
		},
		Resetter: func() { *dst = nil },
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

func scaleQueueCustomStep(cur *verda.QueueLoadTrigger, dst **float64) wizard.Step {
	return wizard.Step{
		Name:        "queue-load-custom",
		Description: fmt.Sprintf("Queue-load threshold (%d..%d)", minQueueLoad, maxQueueLoad),
		Prompt:      wizard.TextInputPrompt,
		DependsOn:   []string{scaleStepSettings, scaleSetQueueLoad},
		ShouldSkip: func(c map[string]any) bool {
			return !scaleSelected(c, scaleSetQueueLoad) || c[scaleSetQueueLoad] != presetCustom
		},
		Default: func(map[string]any) any {
			if cur != nil && cur.Threshold > 0 {
				return strconv.FormatFloat(cur.Threshold, 'f', -1, 64)
			}
			return strconv.Itoa(queueLoadBalanced)
		},
		Validate: func(v any) error {
			q, err := strconv.ParseFloat(strings.TrimSpace(v.(string)), 64)
			if err != nil || q < minQueueLoad || q > maxQueueLoad {
				return fmt.Errorf("must be a number in %d..%d", minQueueLoad, maxQueueLoad)
			}
			return nil
		},
		Setter: func(v any) {
			q, _ := strconv.ParseFloat(strings.TrimSpace(v.(string)), 64)
			*dst = &q
		},
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

// scaleUtilModeStep owns dst's reset. "On" always asks for the threshold
// (prefilled with the current one), so the PATCH never copies a lagging read.
func scaleUtilModeStep(key, desc string, dst **int) wizard.Step {
	return wizard.Step{
		Name:        key,
		Description: desc,
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{scaleStepSettings},
		ShouldSkip:  func(c map[string]any) bool { return !scaleSelected(c, key) },
		Loader:      wizard.StaticChoices(wizard.Choice{Label: "Off", Value: utilOff}, wizard.Choice{Label: "On", Value: utilOn}),
		Setter: func(v any) {
			*dst = nil
			if v.(string) == utilOff {
				*dst = new(0)
			}
		},
		Resetter: func() { *dst = nil },
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}

func scaleUtilThresholdStep(key, kind string, cur *verda.UtilizationTrigger, dst **int) wizard.Step {
	return wizard.Step{
		Name:        key + "-threshold",
		Description: fmt.Sprintf("%s utilization threshold %% (%d..%d)", kind, minUtilThreshold, maxUtilThreshold),
		Prompt:      wizard.TextInputPrompt,
		DependsOn:   []string{scaleStepSettings, key},
		ShouldSkip: func(c map[string]any) bool {
			return !scaleSelected(c, key) || c[key] != utilOn
		},
		Default: func(map[string]any) any {
			if cur != nil && cur.Threshold >= minUtilThreshold {
				return strconv.Itoa(cur.Threshold)
			}
			return "80"
		},
		Validate: func(v any) error {
			n, err := strconv.Atoi(strings.TrimSpace(v.(string)))
			if err != nil || n < minUtilThreshold || n > maxUtilThreshold {
				return fmt.Errorf("must be an integer in %d..%d", minUtilThreshold, maxUtilThreshold)
			}
			return nil
		},
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*dst = &n
		},
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return nil },
	}
}
