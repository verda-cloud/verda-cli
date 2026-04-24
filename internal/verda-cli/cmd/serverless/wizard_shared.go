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

// This file holds step builders and helpers shared between the container and
// batchjob create wizards. Each builder takes a pointer to the field it
// mutates so the same step definition drives both `containerCreateOptions`
// and `batchjobCreateOptions` without an interface layer.

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

// --- Container image ---

func stepImage(target *string) wizard.Step {
	return wizard.Step{
		Name:        "image",
		Description: "Container image (e.g. ghcr.io/org/app:v1.2)",
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

// --- Compute resource (/serverless-compute-resources) ---

func stepCompute(getClient clientFunc, cache *apiCache, target *string) wizard.Step {
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
		Default:  func(_ map[string]any) any { return *target },
		Setter:   func(v any) { *target = v.(string) },
		Resetter: func() { *target = "" },
		IsSet:    func() bool { return *target != "" },
		Value:    func() any { return *target },
	}
}

// --- Compute size (count of GPUs or vCPUs per replica) ---

func stepComputeSize(target *int) wizard.Step {
	return wizard.Step{
		Name:        "compute-size",
		Description: "Compute size (GPUs or vCPUs per replica)",
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

// --- Registry credentials ---

func stepRegistryCreds(getClient clientFunc, cache *apiCache, target *string) wizard.Step {
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

// --- Exposed HTTP port ---

func stepPort(target *int) wizard.Step {
	return wizard.Step{
		Name:        "port",
		Description: "Exposed HTTP port",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return strconv.Itoa(*target) },
		Validate:    parsePortValidator("port"),
		Setter: func(v any) {
			n, _ := strconv.Atoi(strings.TrimSpace(v.(string)))
			*target = n
		},
		Resetter: func() { *target = defaultExposedPort },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Env vars (loop) ---

func stepEnvVars(target *[]string) wizard.Step {
	return wizard.Step{
		Name:        "env-vars",
		Description: "Environment variables (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			for {
				add, err := prompter.Confirm(ctx, fmt.Sprintf("Add environment variable? (have %d)", len(*target)), tui.WithConfirmDefault(false))
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
				*target = append(*target, entry.Name+"="+entry.ValueOrReferenceToSecret)
			}
		},
		Setter:   func(_ any) {},
		Resetter: func() {},
		IsSet:    func() bool { return len(*target) > 0 },
		Value:    func() any { return "" },
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
		Resetter: func() { *target = defaultMaxReplicas },
		IsSet:    func() bool { return false },
		Value:    func() any { return strconv.Itoa(*target) },
	}
}

// --- Request TTL ---

func stepRequestTTL(target *time.Duration) wizard.Step {
	return durationStep("request-ttl", "Request time-to-live (pending queue)", target, defaultRequestTTL)
}

// --- Secret mounts (loop) ---

func stepSecretMounts(getClient clientFunc, cache *apiCache, target *[]string) wizard.Step {
	return wizard.Step{
		Name:        "secret-mounts",
		Description: "Secret mounts (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			for {
				add, err := prompter.Confirm(ctx, fmt.Sprintf("Add a secret mount? (have %d)", len(*target)), tui.WithConfirmDefault(false))
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
				*target = append(*target, mount.SecretName+":"+mount.MountPath)
			}
		},
		Setter:   func(_ any) {},
		Resetter: func() {},
		IsSet:    func() bool { return len(*target) > 0 },
		Value:    func() any { return "" },
	}
}

// --- Generic builders shared by both wizards ---

// durationStep builds a TextInput step that parses a Go duration (0s, 300s,
// 5m, ...). Empty input resets to def.
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

// --- Validators (shared) ---

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
