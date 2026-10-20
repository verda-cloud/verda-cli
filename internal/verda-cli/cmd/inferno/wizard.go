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

package inferno

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

const (
	computeTypeOnDemand = "on-demand"
	computeTypeSpot     = "spot"

	presetInstant   = "instant"
	presetBalanced  = "balanced"
	presetCostSaver = "cost-saver"
	presetCustom    = "custom"

	queueLoadInstant   = 1
	queueLoadBalanced  = 3
	queueLoadCostSaver = 6

	registryPublicValue = "__public__"
)

func buildInfernoDeployFlow(_ context.Context, getClient clientFunc, opts *infernoDeployOptions) *wizard.Flow {
	cache := &apiCache{}
	return &wizard.Flow{
		Name: "inferno-deploy",
		Steps: []wizard.Step{
			stepName(&opts.Name),
			stepImage(&opts.Image),
			stepRegistryCreds(getClient, cache, &opts.RegistryCreds),
			stepComputeType(&opts.Spot),
			stepCompute(getClient, cache, &opts.Compute),
			stepComputeSize(&opts.ComputeSize),
			stepMinReplicas(&opts.MinReplicas),
			stepMaxReplicas(&opts.MaxReplicas),
			stepQueuePreset(&opts.QueuePreset),
			stepQueueLoadCustom(&opts.QueueLoad),
			stepEnvVars(&opts.Env),
		},
	}
}

// --- Deployment name ---

func stepName(target *string) wizard.Step {
	return wizard.Step{
		Name:        "name",
		Description: "Deployment name (URL slug, immutable)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return *target },
		Validate: func(v any) error {
			return validateDeploymentName(strings.TrimSpace(v.(string)))
		},
		Setter:   func(v any) { *target = strings.TrimSpace(v.(string)) },
		Resetter: func() { *target = "" },
		IsSet:    func() bool { return *target != "" },
		Value:    func() any { return *target },
	}
}

// --- Worker image ---

func stepImage(target *string) wizard.Step {
	return wizard.Step{
		Name:        "image",
		Description: "Worker image (e.g. vccr.io/project/model-worker:v1.0)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return *target },
		Validate: func(v any) error {
			img := strings.TrimSpace(v.(string))
			if img == "" {
				return errors.New("image is required")
			}
			return rejectLatestTag(img)
		},
		Setter:   func(v any) { *target = strings.TrimSpace(v.(string)) },
		Resetter: func() { *target = "" },
		IsSet:    func() bool { return *target != "" },
		Value:    func() any { return *target },
	}
}

// --- Registry credentials ---

func stepRegistryCreds(getClient clientFunc, cache *apiCache, target *string) wizard.Step {
	return wizard.Step{
		Name:        "registry-creds",
		Description: "Registry credentials (for private images)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			choices := []wizard.Choice{
				{Label: "Public (no credentials)", Value: registryPublicValue},
			}
			creds, err := withFetchSpinner(ctx, status, "Fetching registry credentials…", func(ctx context.Context) ([]verda.RegistryCredentials, error) {
				return cache.fetchRegistryCreds(ctx, getClient)
			})
			if err != nil {
				return choices, nil //nolint:nilerr // intentional: degrade gracefully if creds fetch fails
			}
			for _, c := range creds {
				if c.Name == registryPublicValue {
					continue
				}
				choices = append(choices, wizard.Choice{Label: c.Name, Value: c.Name})
			}
			return choices, nil
		},
		Default: func(_ map[string]any) any {
			if *target == "" {
				return registryPublicValue
			}
			return *target
		},
		Setter: func(v any) {
			s := v.(string)
			if s == registryPublicValue {
				*target = ""
				return
			}
			*target = s
		},
		Resetter: func() { *target = "" },
		IsSet:    func() bool { return *target != "" },
		Value: func() any {
			if *target == "" {
				return registryPublicValue
			}
			return *target
		},
	}
}

// --- Compute type (on-demand | spot) ---

func stepComputeType(spot *bool) wizard.Step {
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

// --- Compute resource ---

func stepCompute(getClient clientFunc, cache *apiCache, target *string) wizard.Step {
	return wizard.Step{
		Name:        "compute",
		Description: "Compute resource",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			res, err := withFetchSpinner(ctx, status, "Fetching compute resources…", func(ctx context.Context) ([]verda.ComputeResource, error) {
				return cache.fetchComputeResources(ctx, getClient)
			})
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
		Default:  func(_ map[string]any) any { return *target },
		Setter:   func(v any) { *target = v.(string) },
		Resetter: func() { *target = "" },
		IsSet:    func() bool { return *target != "" },
		Value:    func() any { return *target },
	}
}

// --- Compute size ---

func stepComputeSize(target *int) wizard.Step {
	return wizard.Step{
		Name:        "compute-size",
		Description: "Compute size (GPUs per replica)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			if *target > 0 {
				return strconv.Itoa(*target)
			}
			return "1"
		},
		Validate: parsePositiveIntValidator("compute size"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = 0 },
		IsSet:    func() bool { return *target > 0 },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Min replicas ---

func stepMinReplicas(target *int) wizard.Step {
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

// --- Max replicas ---

func stepMaxReplicas(target *int) wizard.Step {
	return wizard.Step{
		Name:        "max-replicas",
		Description: "Max replicas",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(*target) },
		Validate:    parsePositiveIntValidator("max replicas"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = defaultInfernoMaxReplicas },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Queue-load preset ---

func stepQueuePreset(target *string) wizard.Step {
	return wizard.Step{
		Name:        "queue-preset",
		Description: "Queue-load preset",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "Instant", Value: presetInstant, Description: "Scale up as soon as any request arrives"},
			wizard.Choice{Label: "Balanced", Value: presetBalanced, Description: "Short queue wait before scaling up"},
			wizard.Choice{Label: "Cost saver", Value: presetCostSaver, Description: "Fewer replicas; requests may wait longer"},
			wizard.Choice{Label: "Custom", Value: presetCustom, Description: "Specify a queue-load threshold yourself"},
		),
		Default:  func(_ map[string]any) any { return *target },
		Setter:   func(v any) { *target = v.(string) },
		Resetter: func() { *target = presetInstant },
		IsSet:    func() bool { return false },
		Value:    func() any { return *target },
	}
}

// --- Custom queue-load threshold ---

func stepQueueLoadCustom(target *int) wizard.Step {
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

// --- Env vars (interactive loop) ---

func stepEnvVars(target *[]string) wizard.Step {
	return wizard.Step{
		Name:        "env-vars",
		Description: "Environment variables (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			for {
				add, err := prompter.Confirm(ctx, fmt.Sprintf("Add environment variable? (have %d)", len(*target)), tui.WithConfirmDefault(false))
				if err != nil {
					if isPromptCancel(err) {
						return nil, nil
					}
					return nil, err
				}
				if !add {
					return nil, nil
				}
				entry, err := promptEnvVar(ctx, prompter)
				if err != nil {
					return nil, err
				}
				if entry == nil {
					continue
				}
				*target = append(*target, entry.Name+"="+entry.ValueOrReferenceToSecret)
			}
		},
		Setter:   func(_ any) {},
		Resetter: func() {},
		IsSet:    func() bool { return len(*target) > 0 },
		Value:    func() any { return "" },
	}
}

// --- Sub-flows ---

func promptEnvVar(ctx context.Context, prompter tui.Prompter) (*verda.ContainerEnvVar, error) {
	name, err := prompter.TextInput(ctx, "Env name (e.g. HF_HOME)")
	if err != nil {
		if isPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	if !envVarNameRE.MatchString(name) {
		_, _ = prompter.Confirm(ctx, "Invalid env name — use uppercase, digits, underscore, no leading digit. Press Enter to continue.", tui.WithConfirmDefault(true))
		return nil, nil
	}

	value, err := prompter.TextInput(ctx, "Env value")
	if err != nil {
		if isPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	return &verda.ContainerEnvVar{
		Type:                     envTypePlain,
		Name:                     name,
		ValueOrReferenceToSecret: value,
	}, nil
}

// --- Validators ---

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
