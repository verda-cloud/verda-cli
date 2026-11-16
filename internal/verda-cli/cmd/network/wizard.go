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

package network

import (
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

const maxNameLen = 64

func validateName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxNameLen {
		return errNameLength
	}
	return nil
}

func buildNetworkCreateFlow(opts *createOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "network-create",
		Steps: []wizard.Step{
			stepNetworkName(opts),
			stepNetworkMode(opts),
			stepNetworkDefault(opts),
		},
	}
}

func stepNetworkName(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        flagName,
		Description: "Network name",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Validate:    func(v any) error { return validateName(v.(string)) },
		Setter:      func(v any) { opts.Name = strings.TrimSpace(v.(string)) },
		Resetter:    func() { opts.Name = "" },
		IsSet:       func() bool { return opts.Name != "" },
		Value:       func() any { return opts.Name },
	}
}

func stepNetworkMode(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        flagMode,
		Description: "Subnet mode",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "Custom", Value: verda.NetworkModeCustom, Description: "You create the subnets yourself"},
			wizard.Choice{Label: "Auto", Value: verda.NetworkModeAuto, Description: "A subnet is created in a location the first time something is deployed there"},
		),
		Default:  func(_ map[string]any) any { return verda.NetworkModeCustom },
		Setter:   func(v any) { opts.Mode = v.(string) },
		Resetter: func() { opts.Mode = verda.NetworkModeCustom },
		IsSet:    func() bool { return opts.modeSet },
		Value:    func() any { return opts.Mode },
	}
}

func stepNetworkDefault(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        flagDefault,
		Description: "Make it the default network? (VMs that name no network join it)",
		Prompt:      wizard.ConfirmPrompt,
		Default:     func(_ map[string]any) any { return false },
		Setter: func(v any) {
			b, _ := v.(bool)
			opts.Default = b
		},
		Resetter: func() { opts.Default = false },
		IsSet:    func() bool { return opts.defaultSet },
		Value:    func() any { return opts.Default },
	}
}
