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
	"strings"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

// buildBatchjobCreateFlow returns the wizard flow for `verda batchjob create`.
// It reuses nine of the ten steps from the container
// wizard and adds the single batchjob-specific step (deadline). Jobs never
// use spot and have no min/max-replica range, no scaling triggers, no
// concurrency, no healthcheck — so those container-wizard steps are simply
// not included here.
func buildBatchjobCreateFlow(_ context.Context, getClient clientFunc, opts *batchjobCreateOptions) *wizard.Flow {
	cache := &apiCache{}
	return &wizard.Flow{
		Name: "batchjob-create",
		Steps: []wizard.Step{
			stepName(&opts.Name),
			stepCompute(getClient, cache, &opts.Compute),
			stepComputeSize(&opts.ComputeSize),
			stepImage(&opts.Image),
			stepRegistryCreds(getClient, cache, &opts.RegistryCreds),
			stepPort(&opts.Port),
			stepEnvVars(&opts.Env),
			stepMaxReplicas(&opts.MaxReplicas),
			stepBatchjobDeadline(&opts.Deadline),
			stepRequestTTL(&opts.RequestTTL),
			stepSecretMounts(getClient, cache, &opts.SecretMounts),
		},
	}
}

// stepBatchjobDeadline asks for the per-request deadline. Required, 10m..168h;
// the SDK rejects anything outside that range only after the whole wizard.
func stepBatchjobDeadline(target *time.Duration) wizard.Step {
	return wizard.Step{
		Name:        "deadline",
		Description: "Per-request deadline (10m to 168h, e.g. 30m, 1h) — required",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			if *target > 0 {
				return target.String()
			}
			return "30m"
		},
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			d, err := time.ParseDuration(s)
			if err != nil {
				return errors.New("deadline must be a duration (e.g. 30m, 1h)")
			}
			return validateBatchjobDeadline(d)
		},
		Setter: func(v any) {
			d, _ := time.ParseDuration(strings.TrimSpace(v.(string)))
			*target = d
		},
		Resetter: func() { *target = 0 },
		IsSet:    func() bool { return *target > 0 },
		Value:    func() any { return target.String() },
	}
}
