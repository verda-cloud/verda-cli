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
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// Container-specific wizard steps. Steps shared with batchjob live in
// wizard_shared.go; the container flow below wires them up together with
// container-only fields (spot, healthcheck, min replicas, concurrency,
// queue-load presets, CPU/GPU util triggers, scale-up/down delays).

const (
	computeTypeOnDemand = "on-demand"
	computeTypeSpot     = "spot"

	healthcheckOn  = "on"
	healthcheckOff = "off"

	utilOff = "off"

	registryPublicValue = "__public__"
)

// buildContainerCreateFlow returns the full wizard flow for `verda serverless
// container create`. Every step has a matching flag on containerCreateOptions,
// so the same opts struct drives both the wizard and the non-interactive path.
// The final deploy confirmation is NOT a wizard step — the caller prints the
// summary and runs a bare Confirm after the flow returns.
func buildContainerCreateFlow(_ context.Context, getClient clientFunc, opts *containerCreateOptions) *wizard.Flow {
	cache := &apiCache{}
	return &wizard.Flow{
		Name: "container-create",
		Steps: []wizard.Step{
			stepName(&opts.Name),
			stepContainerComputeType(&opts.Spot),
			stepCompute(getClient, cache, &opts.Compute),
			stepComputeSize(&opts.ComputeSize),
			stepImage(&opts.Image),
			stepRegistryCreds(getClient, cache, &opts.RegistryCreds),
			stepPort(&opts.Port),
			stepContainerHealthcheck(&opts.HealthcheckOff),
			stepContainerHealthcheckPort(&opts.HealthcheckPort),
			stepContainerHealthcheckPath(&opts.HealthcheckPath),
			stepEnvVars(&opts.Env),
			stepContainerMinReplicas(&opts.MinReplicas),
			stepMaxReplicas(&opts.MaxReplicas),
			stepContainerConcurrency(&opts.Concurrency),
			stepContainerQueuePreset(&opts.QueuePreset),
			stepContainerQueueLoadCustom(&opts.QueueLoad),
			stepContainerCPUUtil(&opts.CPUUtil),
			stepContainerGPUUtil(&opts.GPUUtil),
			stepContainerScaleUpDelay(&opts.ScaleUpDelay),
			stepContainerScaleDownDelay(&opts.ScaleDownDelay),
			stepRequestTTL(&opts.RequestTTL),
			stepSecretMounts(getClient, cache, &opts.SecretMounts),
		},
	}
}

// --- Compute type (on-demand | spot) ---

func stepContainerComputeType(spot *bool) wizard.Step {
	return wizard.Step{
		Name:        "compute-type",
		Description: "Compute type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "On-Demand", Value: computeTypeOnDemand, Description: "Dedicated compute; runs until paused or deleted"},
			wizard.Choice{Label: "Spot", Value: computeTypeSpot, Description: "Lower price; may be reclaimed at any time"},
		),
		Default: func(_ map[string]any) any {
			if *spot {
				return computeTypeSpot
			}
			return computeTypeOnDemand
		},
		Setter:   func(v any) { *spot = v.(string) == computeTypeSpot },
		Resetter: func() { *spot = false },
		IsSet:    func() bool { return false },
		Value: func() any {
			if *spot {
				return computeTypeSpot
			}
			return computeTypeOnDemand
		},
	}
}

// --- Healthcheck (on/off + port + path) ---

func stepContainerHealthcheck(off *bool) wizard.Step {
	return wizard.Step{
		Name:        "healthcheck",
		Description: "Healthcheck",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "On", Value: healthcheckOn, Description: "Probe the container before routing requests"},
			wizard.Choice{Label: "Off", Value: healthcheckOff, Description: "Route requests immediately"},
		),
		Default: func(_ map[string]any) any {
			if *off {
				return healthcheckOff
			}
			return healthcheckOn
		},
		Setter:   func(v any) { *off = v.(string) == healthcheckOff },
		Resetter: func() { *off = false },
		IsSet:    func() bool { return false },
		Value: func() any {
			if *off {
				return healthcheckOff
			}
			return healthcheckOn
		},
	}
}

func stepContainerHealthcheckPort(port *int) wizard.Step {
	return wizard.Step{
		Name:        "healthcheck-port",
		Description: "Healthcheck port (blank = same as exposed)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		DependsOn:   []string{"healthcheck"},
		ShouldSkip: func(c map[string]any) bool {
			return c["healthcheck"] == healthcheckOff
		},
		Default: func(_ map[string]any) any {
			if *port > 0 {
				return strconv.Itoa(*port)
			}
			return ""
		},
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return nil
			}
			return parsePortValidator("healthcheck port")(v)
		},
		Setter: func(v any) {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				*port = 0
				return
			}
			n, _ := strconv.Atoi(s)
			*port = n
		},
		Resetter: func() { *port = 0 },
		IsSet:    func() bool { return *port > 0 },
		Value:    func() any { return strconv.Itoa(*port) },
	}
}

func stepContainerHealthcheckPath(path *string) wizard.Step {
	return wizard.Step{
		Name:        "healthcheck-path",
		Description: "Healthcheck path",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		DependsOn:   []string{"healthcheck"},
		ShouldSkip: func(c map[string]any) bool {
			return c["healthcheck"] == healthcheckOff
		},
		Default:  func(_ map[string]any) any { return *path },
		Setter:   func(v any) { *path = strings.TrimSpace(v.(string)) },
		Resetter: func() { *path = defaultHealthcheckPath },
		IsSet:    func() bool { return false },
		Value:    func() any { return *path },
	}
}

// --- Min replicas (container-only; batchjob has no min) ---

func stepContainerMinReplicas(target *int) wizard.Step {
	return wizard.Step{
		Name:        "min-replicas",
		Description: "Min replicas (0 = scale-to-zero)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default:     func(_ map[string]any) any { return strconv.Itoa(*target) },
		Validate:    parseNonNegativeIntValidator("min replicas"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = 0 },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Concurrency ---

func stepContainerConcurrency(target *int) wizard.Step {
	return wizard.Step{
		Name:        "concurrency",
		Description: "Concurrent requests per replica (1 for image-gen, higher for LLMs)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(*target) },
		Validate:    parsePositiveIntValidator("concurrency"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = defaultConcurrency },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Queue-load preset + custom value ---

func stepContainerQueuePreset(target *string) wizard.Step {
	return wizard.Step{
		Name:        "queue-preset",
		Description: "Queue-load preset",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "Instant", Value: presetInstant, Description: "Scale up as soon as any request arrives. Minimizes time in queue."},
			wizard.Choice{Label: "Balanced", Value: presetBalanced, Description: "Short queue wait before scaling up. Good for most APIs."},
			wizard.Choice{Label: "Cost saver", Value: presetCostSaver, Description: "Fewer replicas; requests may wait longer in queue."},
			wizard.Choice{Label: "Custom", Value: presetCustom, Description: "Specify a queue-load threshold yourself."},
		),
		Default:  func(_ map[string]any) any { return *target },
		Setter:   func(v any) { *target = v.(string) },
		Resetter: func() { *target = presetBalanced },
		IsSet:    func() bool { return false },
		Value:    func() any { return *target },
	}
}

func stepContainerQueueLoadCustom(target *int) wizard.Step {
	return wizard.Step{
		Name:        "queue-load-custom",
		Description: "Custom queue-load threshold (1..1000)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"queue-preset"},
		ShouldSkip: func(c map[string]any) bool {
			return c["queue-preset"] != presetCustom
		},
		Default: func(_ map[string]any) any {
			if *target > 0 {
				return strconv.Itoa(*target)
			}
			return strconv.Itoa(queueLoadBalanced)
		},
		Validate: func(v any) error {
			n, err := strconv.Atoi(strings.TrimSpace(v.(string)))
			if err != nil || n < 1 || n > 1000 {
				return errors.New("must be an integer in 1..1000")
			}
			return nil
		},
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = 0 },
		IsSet:    func() bool { return *target > 0 },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Utilization triggers (CPU/GPU) ---

func stepContainerCPUUtil(target *int) wizard.Step {
	return utilThresholdStep("cpu-util", "CPU utilization trigger", target)
}

func stepContainerGPUUtil(target *int) wizard.Step {
	return utilThresholdStep("gpu-util", "GPU utilization trigger", target)
}

// utilThresholdStep builds a step that asks "off | <threshold>" as a text
// input. An empty value or "off" maps to 0; any integer in 1..100 enables
// the trigger at that threshold. Shared by CPU and GPU util steps.
func utilThresholdStep(name, desc string, target *int) wizard.Step {
	return wizard.Step{
		Name:        name,
		Description: desc + " (blank = off; else 1..100)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default: func(_ map[string]any) any {
			if *target > 0 {
				return strconv.Itoa(*target)
			}
			return ""
		},
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" || strings.EqualFold(s, utilOff) {
				return nil
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 100 {
				return errors.New("must be blank, 'off', or an integer in 1..100")
			}
			return nil
		},
		Setter: func(v any) {
			s := strings.TrimSpace(v.(string))
			if s == "" || strings.EqualFold(s, utilOff) {
				*target = 0
				return
			}
			n, _ := strconv.Atoi(s)
			*target = n
		},
		Resetter: func() { *target = 0 },
		IsSet:    func() bool { return false },
		Value: func() any {
			if *target > 0 {
				return strconv.Itoa(*target)
			}
			return ""
		},
	}
}

// --- Scale-up / scale-down delays ---

func stepContainerScaleUpDelay(target *time.Duration) wizard.Step {
	return durationStep("scale-up-delay", "Scale-up delay", target, 0)
}

func stepContainerScaleDownDelay(target *time.Duration) wizard.Step {
	return durationStep("scale-down-delay", "Scale-down delay", target, defaultScaleDownDelay)
}
