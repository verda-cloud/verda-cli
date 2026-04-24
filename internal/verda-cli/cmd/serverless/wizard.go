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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

const (
	computeTypeOnDemand = "on-demand"
	computeTypeSpot     = "spot"

	healthcheckOn  = "on"
	healthcheckOff = "off"

	utilOff = "off"
)

// buildContainerCreateFlow returns the full wizard flow for `verda serverless
// container create`. Each step has a matching flag on containerCreateOptions,
// so the same opts struct drives both the wizard and the non-interactive path.
// The final deploy confirmation is NOT a wizard step — the caller prints the
// summary and runs a bare Confirm after the flow returns, so the review card
// has full-width layout control.
func buildContainerCreateFlow(_ context.Context, getClient clientFunc, opts *containerCreateOptions) *wizard.Flow {
	cache := &apiCache{}
	return &wizard.Flow{
		Name: "container-create",
		Steps: []wizard.Step{
			stepContainerName(opts),
			stepContainerComputeType(opts),
			stepContainerCompute(getClient, cache, opts),
			stepContainerComputeSize(opts),
			stepContainerImage(opts),
			stepContainerRegistryCreds(getClient, cache, opts),
			stepContainerPort(opts),
			stepContainerHealthcheck(opts),
			stepContainerHealthcheckPort(opts),
			stepContainerHealthcheckPath(opts),
			stepContainerEnvVars(opts),
			stepContainerMinReplicas(opts),
			stepContainerMaxReplicas(opts),
			stepContainerConcurrency(opts),
			stepContainerQueuePreset(opts),
			stepContainerQueueLoadCustom(opts),
			stepContainerCPUUtil(opts),
			stepContainerGPUUtil(opts),
			stepContainerScaleUpDelay(opts),
			stepContainerScaleDownDelay(opts),
			stepContainerRequestTTL(opts),
			stepContainerSecretMounts(getClient, cache, opts),
		},
	}
}

// --- 1. Deployment name ---

func stepContainerName(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "name",
		Description: "Deployment name (URL slug, immutable)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return opts.Name },
		Validate: func(v any) error {
			return validateDeploymentName(strings.TrimSpace(v.(string)))
		},
		Setter:   func(v any) { opts.Name = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Name = "" },
		IsSet:    func() bool { return opts.Name != "" },
		Value:    func() any { return opts.Name },
	}
}

// --- 2. Compute type (on-demand | spot) ---

func stepContainerComputeType(opts *containerCreateOptions) wizard.Step {
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
			if opts.Spot {
				return computeTypeSpot
			}
			return computeTypeOnDemand
		},
		Setter:   func(v any) { opts.Spot = v.(string) == computeTypeSpot },
		Resetter: func() { opts.Spot = false },
		IsSet:    func() bool { return false }, // always prompt
		Value: func() any {
			if opts.Spot {
				return computeTypeSpot
			}
			return computeTypeOnDemand
		},
	}
}

// --- 3. Compute resource (GPU/CPU pick from /serverless-compute-resources) ---

func stepContainerCompute(getClient clientFunc, cache *apiCache, opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "compute",
		Description: "Compute resource",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			res, err := cache.fetchComputeResources(ctx, getClient)
			if err != nil {
				return nil, err
			}
			choices := make([]wizard.Choice, 0, len(res))
			for i := range res {
				r := &res[i]
				desc := "available"
				if !r.IsAvailable {
					desc = "unavailable"
				}
				choices = append(choices, wizard.Choice{
					Label:       fmt.Sprintf("%s  (size %d)", r.Name, r.Size),
					Value:       r.Name,
					Description: desc,
				})
			}
			if len(choices) == 0 {
				return nil, errors.New("no serverless compute resources available")
			}
			return choices, nil
		},
		Default:  func(_ map[string]any) any { return opts.Compute },
		Setter:   func(v any) { opts.Compute = v.(string) },
		Resetter: func() { opts.Compute = "" },
		IsSet:    func() bool { return opts.Compute != "" },
		Value:    func() any { return opts.Compute },
	}
}

// --- 4. Compute size (count of GPUs or vCPUs) ---

func stepContainerComputeSize(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "compute-size",
		Description: "Compute size (number of GPUs/vCPUs per replica)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			if opts.ComputeSize > 0 {
				return strconv.Itoa(opts.ComputeSize)
			}
			return "1"
		},
		Validate: parsePositiveIntValidator("compute size"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			opts.ComputeSize = n
		},
		Resetter: func() { opts.ComputeSize = 0 },
		IsSet:    func() bool { return opts.ComputeSize > 0 },
		Value:    func() any { return strconv.Itoa(opts.ComputeSize) },
	}
}

// --- 5. Container image ---

func stepContainerImage(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "image",
		Description: "Container image (e.g. ghcr.io/org/app:v1.2)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return opts.Image },
		Validate: func(v any) error {
			img := strings.TrimSpace(v.(string))
			if img == "" {
				return errors.New("image is required")
			}
			return rejectLatestTag(img)
		},
		Setter:   func(v any) { opts.Image = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Image = "" },
		IsSet:    func() bool { return opts.Image != "" },
		Value:    func() any { return opts.Image },
	}
}

// --- 6. Registry credentials ---

const registryPublicValue = "__public__"

func stepContainerRegistryCreds(getClient clientFunc, cache *apiCache, opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "registry-creds",
		Description: "Registry credentials (for private images)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			choices := []wizard.Choice{
				{Label: "Public (no credentials)", Value: registryPublicValue},
			}
			creds, err := cache.fetchRegistryCreds(ctx, getClient)
			if err != nil {
				// Non-fatal: offer public-only and let the user continue.
				return choices, nil //nolint:nilerr // degrade gracefully on missing permissions
			}
			for _, c := range creds {
				choices = append(choices, wizard.Choice{
					Label: c.Name,
					Value: c.Name,
				})
			}
			return choices, nil
		},
		Default: func(_ map[string]any) any {
			if opts.RegistryCreds == "" {
				return registryPublicValue
			}
			return opts.RegistryCreds
		},
		Setter: func(v any) {
			s := v.(string)
			if s == registryPublicValue {
				opts.RegistryCreds = ""
				return
			}
			opts.RegistryCreds = s
		},
		Resetter: func() { opts.RegistryCreds = "" },
		IsSet:    func() bool { return opts.RegistryCreds != "" },
		Value: func() any {
			if opts.RegistryCreds == "" {
				return registryPublicValue
			}
			return opts.RegistryCreds
		},
	}
}

// --- 7. Exposed HTTP port ---

func stepContainerPort(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "port",
		Description: "Exposed HTTP port",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(opts.Port) },
		Validate:    parsePortValidator("port"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			opts.Port = n
		},
		Resetter: func() { opts.Port = defaultExposedPort },
		IsSet:    func() bool { return false }, // always show the step; default carries the pre-set value
		Value:    func() any { return strconv.Itoa(opts.Port) },
	}
}

// --- 8-10. Healthcheck (on/off + port + path) ---

func stepContainerHealthcheck(opts *containerCreateOptions) wizard.Step {
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
			if opts.HealthcheckOff {
				return healthcheckOff
			}
			return healthcheckOn
		},
		Setter:   func(v any) { opts.HealthcheckOff = v.(string) == healthcheckOff },
		Resetter: func() { opts.HealthcheckOff = false },
		IsSet:    func() bool { return false },
		Value: func() any {
			if opts.HealthcheckOff {
				return healthcheckOff
			}
			return healthcheckOn
		},
	}
}

func stepContainerHealthcheckPort(opts *containerCreateOptions) wizard.Step {
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
			if opts.HealthcheckPort > 0 {
				return strconv.Itoa(opts.HealthcheckPort)
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
				opts.HealthcheckPort = 0
				return
			}
			n, _ := strconv.Atoi(s)
			opts.HealthcheckPort = n
		},
		Resetter: func() { opts.HealthcheckPort = 0 },
		IsSet:    func() bool { return opts.HealthcheckPort > 0 },
		Value:    func() any { return strconv.Itoa(opts.HealthcheckPort) },
	}
}

func stepContainerHealthcheckPath(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "healthcheck-path",
		Description: "Healthcheck path",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		DependsOn:   []string{"healthcheck"},
		ShouldSkip: func(c map[string]any) bool {
			return c["healthcheck"] == healthcheckOff
		},
		Default:  func(_ map[string]any) any { return opts.HealthcheckPath },
		Setter:   func(v any) { opts.HealthcheckPath = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.HealthcheckPath = defaultHealthcheckPath },
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.HealthcheckPath },
	}
}

// --- 11. Env vars (loop) ---

func stepContainerEnvVars(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "env-vars",
		Description: "Environment variables (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			// Loop: add env vars until the user says "done".
			for {
				add, err := prompter.Confirm(ctx, fmt.Sprintf("Add environment variable? (have %d)", len(opts.Env)), tui.WithConfirmDefault(false))
				if err != nil || !add {
					return nil, nil //nolint:nilerr // prompter cancel is a clean exit
				}
				entry, err := promptEnvVar(ctx, prompter)
				if err != nil {
					return nil, err
				}
				if entry == nil {
					continue
				}
				opts.Env = append(opts.Env, entry.Name+"="+entry.ValueOrReferenceToSecret)
			}
		},
		Setter:   func(_ any) {},
		Resetter: func() {},
		IsSet:    func() bool { return len(opts.Env) > 0 },
		Value:    func() any { return "" },
	}
}

// --- 12. Min replicas ---

func stepContainerMinReplicas(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "min-replicas",
		Description: "Min replicas (0 = scale-to-zero)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default:     func(_ map[string]any) any { return strconv.Itoa(opts.MinReplicas) },
		Validate:    parseNonNegativeIntValidator("min replicas"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			opts.MinReplicas = n
		},
		Resetter: func() { opts.MinReplicas = 0 },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(opts.MinReplicas) },
	}
}

// --- 13. Max replicas ---

func stepContainerMaxReplicas(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "max-replicas",
		Description: "Max replicas",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(opts.MaxReplicas) },
		Validate:    parsePositiveIntValidator("max replicas"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			opts.MaxReplicas = n
		},
		Resetter: func() { opts.MaxReplicas = defaultMaxReplicas },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(opts.MaxReplicas) },
	}
}

// --- 14. Concurrent requests per replica ---

func stepContainerConcurrency(opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "concurrency",
		Description: "Concurrent requests per replica (1 for image-gen, higher for LLMs)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(opts.Concurrency) },
		Validate:    parsePositiveIntValidator("concurrency"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			opts.Concurrency = n
		},
		Resetter: func() { opts.Concurrency = defaultConcurrency },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(opts.Concurrency) },
	}
}

// --- 15. Queue-load preset ---

func stepContainerQueuePreset(opts *containerCreateOptions) wizard.Step {
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
		Default:  func(_ map[string]any) any { return opts.QueuePreset },
		Setter:   func(v any) { opts.QueuePreset = v.(string) },
		Resetter: func() { opts.QueuePreset = presetBalanced },
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.QueuePreset },
	}
}

// --- 16. Custom queue-load (only when preset == custom) ---

func stepContainerQueueLoadCustom(opts *containerCreateOptions) wizard.Step {
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
			if opts.QueueLoad > 0 {
				return strconv.Itoa(opts.QueueLoad)
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
			opts.QueueLoad = n
		},
		Resetter: func() { opts.QueueLoad = 0 },
		IsSet:    func() bool { return opts.QueueLoad > 0 },
		Value:    func() any { return strconv.Itoa(opts.QueueLoad) },
	}
}

// --- 17. CPU utilization trigger ---

func stepContainerCPUUtil(opts *containerCreateOptions) wizard.Step {
	return utilThresholdStep("cpu-util", "CPU utilization trigger",
		&opts.CPUUtil)
}

// --- 18. GPU utilization trigger ---

func stepContainerGPUUtil(opts *containerCreateOptions) wizard.Step {
	return utilThresholdStep("gpu-util", "GPU utilization trigger",
		&opts.GPUUtil)
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

// --- 19. Scale-up delay ---

func stepContainerScaleUpDelay(opts *containerCreateOptions) wizard.Step {
	return durationStep("scale-up-delay", "Scale-up delay", &opts.ScaleUpDelay, 0)
}

// --- 20. Scale-down delay ---

func stepContainerScaleDownDelay(opts *containerCreateOptions) wizard.Step {
	return durationStep("scale-down-delay", "Scale-down delay", &opts.ScaleDownDelay, defaultScaleDownDelay)
}

// --- 21. Request TTL ---

func stepContainerRequestTTL(opts *containerCreateOptions) wizard.Step {
	return durationStep("request-ttl", "Request time-to-live (pending queue)", &opts.RequestTTL, defaultRequestTTL)
}

func durationStep(name, desc string, target *time.Duration, def time.Duration) wizard.Step {
	return wizard.Step{
		Name:        name,
		Description: desc + " (e.g. 0s, 300s, 5m)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default: func(_ map[string]any) any {
			if *target > 0 {
				return target.String()
			}
			return def.String()
		},
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return nil
			}
			d, err := time.ParseDuration(s)
			if err != nil || d < 0 {
				return errors.New("must be a non-negative duration (e.g. 0s, 300s, 5m)")
			}
			return nil
		},
		Setter: func(v any) {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				*target = def
				return
			}
			d, _ := time.ParseDuration(s)
			*target = d
		},
		Resetter: func() { *target = def },
		IsSet:    func() bool { return false },
		Value:    func() any { return target.String() },
	}
}

// --- 22. Secret mounts (loop) ---

func stepContainerSecretMounts(getClient clientFunc, cache *apiCache, opts *containerCreateOptions) wizard.Step {
	return wizard.Step{
		Name:        "secret-mounts",
		Description: "Secret mounts (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			for {
				add, err := prompter.Confirm(ctx, fmt.Sprintf("Add a secret mount? (have %d)", len(opts.SecretMounts)), tui.WithConfirmDefault(false))
				if err != nil || !add {
					return nil, nil //nolint:nilerr // prompter cancel is a clean exit
				}
				secrets, _ := cache.fetchSecrets(ctx, getClient)
				fileSecrets, _ := cache.fetchFileSecrets(ctx, getClient)
				if len(secrets)+len(fileSecrets) == 0 {
					_, _ = prompter.Confirm(ctx, "No secrets available in this project. Press Enter to continue.", tui.WithConfirmDefault(true))
					return nil, nil
				}
				mount, err := promptSecretMount(ctx, prompter, secrets, fileSecrets)
				if err != nil {
					return nil, err
				}
				if mount == nil {
					continue
				}
				opts.SecretMounts = append(opts.SecretMounts, mount.SecretName+":"+mount.MountPath)
			}
		},
		Setter:   func(_ any) {},
		Resetter: func() {},
		IsSet:    func() bool { return len(opts.SecretMounts) > 0 },
		Value:    func() any { return "" },
	}
}

// --- Shared validators ---

func parsePositiveIntValidator(field string) func(any) error {
	return func(v any) error {
		n, err := strconv.Atoi(strings.TrimSpace(v.(string)))
		if err != nil || n < 1 {
			return fmt.Errorf("%s must be a positive integer", field)
		}
		return nil
	}
}

func parseNonNegativeIntValidator(field string) func(any) error {
	return func(v any) error {
		n, err := strconv.Atoi(strings.TrimSpace(v.(string)))
		if err != nil || n < 0 {
			return fmt.Errorf("%s must be an integer >= 0", field)
		}
		return nil
	}
}

func parsePortValidator(field string) func(any) error {
	return func(v any) error {
		n, err := strconv.Atoi(strings.TrimSpace(v.(string)))
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("%s must be an integer in 1..65535", field)
		}
		return nil
	}
}
