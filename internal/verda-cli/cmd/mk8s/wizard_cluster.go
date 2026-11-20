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

// runWizard returns a stopError as written, not wrapped with the step name.
func runWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, flow *wizard.Flow) error {
	err := newWizardEngine(f, ioStreams).Run(ctx, flow)
	if stop, ok := errors.AsType[*stopError](err); ok {
		return stop
	}
	return err
}

// wizardLayout has no hint bar view: the engine renders its own below each prompt.
func wizardLayout() []wizard.ViewDef {
	return []wizard.ViewDef{
		{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
	}
}

// The SDK and the API own the real rules (name format and length, CIDR size and
// overlap); the wizard only rejects input that cannot be sent at all.
func validateName(s string) error {
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

func wizardText(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

// stringStep binds a required step to *dst. set is fixed when the flow is
// built: a value from a flag skips the step.
func stringStep(name, desc string, prompt wizard.PromptType, dst *string, set bool) wizard.Step {
	return wizard.Step{
		Name: name, Description: desc, Prompt: prompt, Required: true,
		Setter:   func(v any) { *dst = wizardText(v) },
		Resetter: func() { *dst = "" },
		IsSet:    func() bool { return set },
		Value:    func() any { return *dst },
	}
}

func textStep(name, desc string, dst *string, set bool, validate func(string) error) wizard.Step {
	s := stringStep(name, desc, wizard.TextInputPrompt, dst, set)
	s.Validate = func(v any) error { return validate(wizardText(v)) }
	return s
}

func selectStep(name, desc string, dst *string, set bool, loader wizard.LoaderFunc, dependsOn ...string) wizard.Step {
	s := stringStep(name, desc, wizard.SelectPrompt, dst, set)
	s.Loader, s.DependsOn = loader, dependsOn
	return s
}

// cidrStep is optional: Enter keeps def, and Back restores it.
func cidrStep(name, desc string, dst *string, set bool, def string) wizard.Step {
	s := textStep(name, desc, dst, set, func(t string) error { return validateCIDR(t, def) })
	s.Required = false
	s.Default = func(map[string]any) any { return def }
	s.Resetter = func() { *dst = def }
	return s
}

// buildClusterCreateFlow asks for every value the flags did not give. Each step
// loads its choices when it is reached, through the shared cache.
func buildClusterCreateFlow(opts *clusterCreateOptions, cache *wizardCache) *wizard.Flow {
	return &wizard.Flow{
		Name:   "mk8s-cluster-create",
		Layout: wizardLayout(),
		Steps: []wizard.Step{
			textStep(flagName, "Cluster name (unique in the project)", &opts.Name, opts.Name != "", validateName),
			selectStep(flagLocation, "Location", &opts.Location, opts.Location != "", locationLoader(cache)),
			textStep(flagKubernetesVersion, "Kubernetes version (for example 1.37.0)", &opts.Version, opts.Version != "", validateVersion),
			selectStep(flagNetwork, "Private network", &opts.Network, opts.Network != "", networkLoader(cache)),
			selectStep(flagSubnet, "Subnet (in the network and location you chose)", &opts.Subnet, opts.Subnet != "",
				subnetLoader(opts, cache), flagNetwork, flagLocation),
			cidrStep(flagPodCIDR, "Pod IP range (CIDR)", &opts.PodCIDR, opts.podCIDRSet, defaultPodCIDR),
			cidrStep(flagServiceCIDR, "Service IP range (CIDR)", &opts.ServiceCIDR, opts.serviceCIDRSet, defaultServiceCIDR),
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
