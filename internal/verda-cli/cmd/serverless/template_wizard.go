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
	"strings"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// ContainerTemplateResult holds the container template wizard's output.
// Field-for-field with the create flags so the converter in cmd/template is
// mechanical; Description is template-only (not a create flag).
type ContainerTemplateResult struct {
	Spot            bool
	Compute         string
	ComputeSize     int
	Image           string
	RegistryCreds   string
	Port            int
	HealthcheckOff  bool
	HealthcheckPort int
	HealthcheckPath string
	Env             []string // KEY=VALUE
	EnvSecret       []string // KEY=SECRET_NAME
	Entrypoint      []string
	Cmd             []string
	MinReplicas     int
	MaxReplicas     int
	Concurrency     int
	QueuePreset     string
	QueueLoad       int
	CPUUtil         int
	GPUUtil         int
	ScaleUpDelay    time.Duration
	ScaleDownDelay  time.Duration
	RequestTTL      time.Duration
	SecretMounts    []string // SECRET:/path
	Description     string
}

// RunContainerTemplateWizard runs the container create flow minus the name
// step (deployment names are immutable and per-deployment), plus a template
// description step. Returns (nil, nil) on user cancel, matching
// vm.RunTemplateWizard's contract.
func RunContainerTemplateWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (*ContainerTemplateResult, error) {
	opts := &containerCreateOptions{
		Port:            defaultExposedPort,
		HealthcheckPath: defaultHealthcheckPath,
		MaxReplicas:     defaultMaxReplicas,
		Concurrency:     defaultConcurrency,
		QueuePreset:     presetBalanced,
		ScaleDownDelay:  defaultScaleDownDelay,
		RequestTTL:      defaultRequestTTL,
	}
	var description string

	cache := &apiCache{}
	flow := &wizard.Flow{
		Name: "container-template",
		Steps: []wizard.Step{
			stepContainerComputeType(&opts.Spot),
			stepCompute(f.VerdaClient, cache, &opts.Compute),
			stepComputeSize(&opts.ComputeSize),
			stepImage(&opts.Image),
			stepRegistryCreds(f.VerdaClient, cache, &opts.RegistryCreds),
			stepPort(&opts.Port),
			stepContainerHealthcheck(&opts.HealthcheckOff),
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
			stepSecretMounts(f.VerdaClient, cache, &opts.SecretMounts),
			stepTemplateDescription(&description),
		},
	}

	engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
	if err := engine.Run(ctx, flow); err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	return containerOptsToTemplateResult(opts, description), nil
}

func containerOptsToTemplateResult(o *containerCreateOptions, description string) *ContainerTemplateResult {
	return &ContainerTemplateResult{
		Spot:            o.Spot,
		Compute:         o.Compute,
		ComputeSize:     o.ComputeSize,
		Image:           o.Image,
		RegistryCreds:   o.RegistryCreds,
		Port:            o.Port,
		HealthcheckOff:  o.HealthcheckOff,
		HealthcheckPort: o.HealthcheckPort,
		HealthcheckPath: o.HealthcheckPath,
		Env:             o.Env,
		EnvSecret:       o.EnvSecret,
		Entrypoint:      o.Entrypoint,
		Cmd:             o.Cmd,
		MinReplicas:     o.MinReplicas,
		MaxReplicas:     o.MaxReplicas,
		Concurrency:     o.Concurrency,
		QueuePreset:     o.QueuePreset,
		QueueLoad:       o.QueueLoad,
		CPUUtil:         o.CPUUtil,
		GPUUtil:         o.GPUUtil,
		ScaleUpDelay:    o.ScaleUpDelay,
		ScaleDownDelay:  o.ScaleDownDelay,
		RequestTTL:      o.RequestTTL,
		SecretMounts:    o.SecretMounts,
		Description:     description,
	}
}

// stepTemplateDescription is a plain optional text input; sibling of the VM
// wizard's template-description step.
func stepTemplateDescription(target *string) wizard.Step {
	return wizard.Step{
		Name:        "description",
		Description: "Template description (optional)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default:     func(_ map[string]any) any { return *target },
		Setter:      func(v any) { *target = strings.TrimSpace(v.(string)) },
		Resetter:    func() { *target = "" },
		IsSet:       func() bool { return false },
		Value:       func() any { return *target },
	}
}
