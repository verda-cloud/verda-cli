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
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const maxNameLen = 64

var errNameTaken = errors.New("the network already has a subnet with this name")

func validateName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxNameLen {
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

const (
	stepLocation = "location"
	stepCIDR     = "cidr"
)

// buildSubnetCreateFlow: network, location, name, CIDR, default. Flags skip their steps.
func buildSubnetCreateFlow(opts *subnetCreateOptions, data *subnetCreateData) *wizard.Flow {
	networkSet, locationSet := opts.Network != "", opts.Location != ""
	nameSet, cidrSet := opts.Name != "", opts.CIDR != ""
	return &wizard.Flow{
		Name: "subnet-create",
		Steps: []wizard.Step{
			{
				Name:        flagNetwork,
				Description: "Private network",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				Loader:      wizard.StaticChoices(networkChoices(data.networks)...),
				Default: func(_ map[string]any) any {
					if n := cmdutil.DefaultNetwork(data.networks); n != nil {
						return n.ID
					}
					return ""
				},
				Setter:   func(v any) { opts.Network, _ = v.(string) },
				Resetter: func() { opts.Network = "" },
				IsSet:    func() bool { return networkSet },
				Value:    func() any { return opts.Network },
			},
			{
				Name:        stepLocation,
				Description: "Location (only locations with private networks)",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				Loader:      wizard.StaticChoices(locationChoices(data.locations)...),
				Setter:      func(v any) { opts.Location, _ = v.(string) },
				Resetter:    func() { opts.Location = "" },
				IsSet:       func() bool { return locationSet },
				Value:       func() any { return opts.Location },
			},
			{
				Name:        flagName,
				Description: "Subnet name",
				Prompt:      wizard.TextInputPrompt,
				DependsOn:   []string{flagNetwork, stepLocation},
				Default:     func(_ map[string]any) any { return data.defaultName(opts.Network, opts.Location) },
				Validate: func(v any) error {
					name := strings.TrimSpace(v.(string))
					if err := validateName(name); err != nil {
						return err
					}
					if cmdutil.FindSubnet(data.subnets[opts.Network], name) != nil {
						return errNameTaken
					}
					return nil
				},
				Setter:   func(v any) { opts.Name = strings.TrimSpace(v.(string)) },
				Resetter: func() { opts.Name = "" },
				IsSet:    func() bool { return nameSet },
				Value:    func() any { return opts.Name },
			},
			{
				Name:        stepCIDR,
				Description: "IPv4 range (CIDR)",
				Prompt:      wizard.TextInputPrompt,
				DependsOn:   []string{flagNetwork},
				Default: func(_ map[string]any) any {
					if p, err := data.suggestCIDR(opts.Network); err == nil {
						return p.String()
					}
					return ""
				},
				Validate: func(v any) error { return data.checkCIDR(opts.Network, strings.TrimSpace(v.(string))) },
				Setter:   func(v any) { opts.CIDR = strings.TrimSpace(v.(string)) },
				Resetter: func() { opts.CIDR = "" },
				IsSet:    func() bool { return cidrSet },
				Value:    func() any { return opts.CIDR },
			},
			{
				Name:        flagDefault,
				Description: "Make it the default subnet of this location?",
				Prompt:      wizard.ConfirmPrompt,
				DependsOn:   []string{flagNetwork, stepLocation},
				ShouldSkip:  func(_ map[string]any) bool { return data.firstInLocation(opts.Network, opts.Location) },
				Default:     func(_ map[string]any) any { return false },
				Setter: func(v any) {
					b, _ := v.(bool)
					opts.Default = b
				},
				Resetter: func() { opts.Default = false },
				IsSet:    func() bool { return opts.defaultSet },
				Value:    func() any { return opts.Default },
			},
		},
	}
}

func networkChoices(networks []verda.PrivateNetwork) []wizard.Choice {
	choices := make([]wizard.Choice, len(networks))
	for i := range networks {
		desc := networks[i].ID + ", " + networks[i].Mode
		if networks[i].IsDefault {
			desc += ", default"
		}
		choices[i] = wizard.Choice{Label: networks[i].Name, Value: networks[i].ID, Description: desc}
	}
	return choices
}

func locationChoices(locations []verda.Location) []wizard.Choice {
	choices := make([]wizard.Choice, len(locations))
	for i := range locations {
		choices[i] = wizard.Choice{Label: locations[i].Code, Value: locations[i].Code, Description: locations[i].Name}
	}
	return choices
}
