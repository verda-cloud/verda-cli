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

package mk8s

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// newWizardEngine builds the wizard engine; a var so tests can drive it with canned answers.
var newWizardEngine = func(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *wizard.Engine {
	return wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
}

// wizardLayout has no hint bar view: the engine renders its own below each prompt.
func wizardLayout() []wizard.ViewDef {
	return []wizard.ViewDef{
		{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
	}
}

// The SDK and the API own the real rules (name format and length, CIDR size and
// overlap); the wizard only rejects input that cannot be sent at all.
func validateClusterName(s string) error {
	if s == "" {
		return errors.New("name must not be empty")
	}
	return nil
}

func validateCIDR(s, example string) error {
	if _, err := netip.ParsePrefix(s); err != nil {
		return fmt.Errorf("enter a range in CIDR form, for example %s", example)
	}
	return nil
}

func validateVersion(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("enter a Kubernetes version, for example 1.37.0")
	}
	return nil
}

// buildClusterCreateFlow asks for every value the flags did not give. Each step
// loads its choices when it is reached, through the shared cache.
func buildClusterCreateFlow(opts *clusterCreateOptions, cache *wizardCache) *wizard.Flow {
	nameSet, locationSet, versionSet := opts.Name != "", opts.Location != "", opts.Version != ""
	networkSet, subnetSet := opts.Network != "", opts.Subnet != ""
	podSet, serviceSet := opts.podCIDRSet, opts.serviceCIDRSet
	text := func(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

	return &wizard.Flow{
		Name:   "mk8s-cluster-create",
		Layout: wizardLayout(),
		Steps: []wizard.Step{
			{
				Name:        flagName,
				Description: "Cluster name (unique in the project)",
				Prompt:      wizard.TextInputPrompt,
				Required:    true,
				Validate:    func(v any) error { return validateClusterName(text(v)) },
				Setter:      func(v any) { opts.Name = text(v) },
				Resetter:    func() { opts.Name = "" },
				IsSet:       func() bool { return nameSet },
				Value:       func() any { return opts.Name },
			},
			{
				Name:        flagLocation,
				Description: "Location",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				Loader:      locationLoader(cache),
				Setter:      func(v any) { opts.Location = text(v) },
				Resetter:    func() { opts.Location = "" },
				IsSet:       func() bool { return locationSet },
				Value:       func() any { return opts.Location },
			},
			{
				Name:        flagKubernetesVersion,
				Description: "Kubernetes version (for example 1.37.0)",
				Prompt:      wizard.TextInputPrompt,
				Required:    true,
				Validate:    func(v any) error { return validateVersion(text(v)) },
				Setter:      func(v any) { opts.Version = text(v) },
				Resetter:    func() { opts.Version = "" },
				IsSet:       func() bool { return versionSet },
				Value:       func() any { return opts.Version },
			},
			{
				Name:        flagNetwork,
				Description: "Private network",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				Loader:      networkLoader(cache),
				Setter:      func(v any) { opts.Network = text(v) },
				Resetter:    func() { opts.Network = "" },
				IsSet:       func() bool { return networkSet },
				Value:       func() any { return opts.Network },
			},
			{
				Name:        flagSubnet,
				Description: "Subnet (in the network and location you chose)",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				DependsOn:   []string{flagNetwork, flagLocation},
				Loader:      subnetLoader(opts, cache),
				Setter:      func(v any) { opts.Subnet = text(v) },
				Resetter:    func() { opts.Subnet = "" },
				IsSet:       func() bool { return subnetSet },
				Value:       func() any { return opts.Subnet },
			},
			{
				Name:        flagPodCIDR,
				Description: "Pod IP range (CIDR)",
				Prompt:      wizard.TextInputPrompt,
				Default:     func(_ map[string]any) any { return defaultPodCIDR },
				Validate:    func(v any) error { return validateCIDR(text(v), defaultPodCIDR) },
				Setter:      func(v any) { opts.PodCIDR = text(v) },
				Resetter:    func() { opts.PodCIDR = defaultPodCIDR },
				IsSet:       func() bool { return podSet },
				Value:       func() any { return opts.PodCIDR },
			},
			{
				Name:        flagServiceCIDR,
				Description: "Service IP range (CIDR)",
				Prompt:      wizard.TextInputPrompt,
				Default:     func(_ map[string]any) any { return defaultServiceCIDR },
				Validate:    func(v any) error { return validateCIDR(text(v), defaultServiceCIDR) },
				Setter:      func(v any) { opts.ServiceCIDR = text(v) },
				Resetter:    func() { opts.ServiceCIDR = defaultServiceCIDR },
				IsSet:       func() bool { return serviceSet },
				Value:       func() any { return opts.ServiceCIDR },
			},
		},
	}
}

func locationLoader(cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		locations, err := cache.enabledLocations(ctx)
		if err != nil {
			return nil, err
		}
		if len(locations) == 0 {
			return nil, stopMessage("Managed Kubernetes is enabled for this project, but no location is configured.")
		}
		choices := make([]wizard.Choice, len(locations))
		for i, code := range locations {
			choices[i] = wizard.Choice{Label: code, Value: code}
		}
		return choices, nil
	}
}

func networkLoader(cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		networks, err := cache.privateNetworks(ctx)
		if err != nil {
			return nil, err
		}
		if len(networks) == 0 {
			return nil, stopMessage("No private network found. Create one with `verda network create`.")
		}
		choices := make([]wizard.Choice, len(networks))
		for i := range networks {
			choices[i] = wizard.Choice{Label: networks[i].Name, Value: networks[i].ID, Description: networks[i].ID + ", " + networks[i].Mode}
		}
		return choices, nil
	}
}

// subnetLoader lists the subnets of the chosen network that are in the chosen location.
func subnetLoader(opts *clusterCreateOptions, cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		subnets, err := cache.networkSubnets(ctx, opts.Network)
		if err != nil {
			return nil, err
		}
		// Only for the stop message below: a network given by --network was never listed.
		_, _ = cache.privateNetworks(ctx)
		var choices []wizard.Choice
		for i := range subnets {
			if subnets[i].Location.Code == opts.Location {
				choices = append(choices, wizard.Choice{Label: subnets[i].Name, Value: subnets[i].ID, Description: subnets[i].ID + ", " + subnets[i].CIDR})
			}
		}
		if len(choices) == 0 {
			return nil, stopMessage(fmt.Sprintf("Network %s has no subnet in %s. Create one with `verda network subnet create`.",
				cache.networkName(opts.Network), opts.Location))
		}
		return choices, nil
	}
}
