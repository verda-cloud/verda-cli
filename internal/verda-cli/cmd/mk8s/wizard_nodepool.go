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
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

func validateReplicas(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return errors.New("enter a whole number, 0 or more")
	}
	return nil
}

func validateName(s string) error {
	if s == "" {
		return errors.New("name must not be empty")
	}
	return nil
}

// buildNodePoolCreateFlow asks for every value the flags did not give. The
// cluster is already known when it came from --cluster.
func buildNodePoolCreateFlow(opts *nodePoolCreateOptions, cache *wizardCache) *wizard.Flow {
	clusterSet, nameSet, typeSet := opts.Cluster != "", opts.Name != "", opts.InstanceType != ""
	// -l as the user gave it. The cluster step sets the location from the
	// cluster it picks, and going Back must undo that, not keep it.
	locationFlag := opts.Location
	keysSet, replicasSet := len(opts.SSHKeys) > 0, opts.replicasSet
	text := func(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }

	return &wizard.Flow{
		Name:   "mk8s-node-pool-create",
		Layout: wizardLayout(),
		Steps: []wizard.Step{
			{
				Name:        flagCluster,
				Description: "Cluster",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				Loader:      clusterLoader(locationFlag, cache),
				Setter: func(v any) {
					opts.Cluster = text(v)
					if locationFlag == "" {
						opts.Location = cache.clusterLocation(opts.Cluster)
					}
				},
				Resetter: func() {
					opts.Cluster = ""
					opts.Location = locationFlag
				},
				IsSet: func() bool { return clusterSet },
				Value: func() any { return opts.Cluster },
			},
			{
				Name:        flagName,
				Description: "Node pool name (unique in the project, not only in the cluster)",
				Prompt:      wizard.TextInputPrompt,
				Required:    true,
				Validate:    func(v any) error { return validateName(text(v)) },
				Setter:      func(v any) { opts.Name = text(v) },
				Resetter:    func() { opts.Name = "" },
				IsSet:       func() bool { return nameSet },
				Value:       func() any { return opts.Name },
			},
			{
				Name:        flagInstanceType,
				Description: "Instance type",
				Prompt:      wizard.SelectPrompt,
				Required:    true,
				DependsOn:   []string{flagCluster},
				Loader:      instanceTypeLoader(opts, cache),
				Setter:      func(v any) { opts.InstanceType = text(v) },
				Resetter:    func() { opts.InstanceType = "" },
				IsSet:       func() bool { return typeSet },
				Value:       func() any { return opts.InstanceType },
			},
			{
				Name:        flagSSHKey,
				Description: "SSH keys for the workers",
				Prompt:      wizard.MultiSelectPrompt,
				Required:    true,
				Loader:      sshKeyLoader(cache),
				MinError:    func(int) string { return "select at least one SSH key" },
				Setter:      func(v any) { opts.SSHKeys, _ = v.([]string) },
				Resetter:    func() { opts.SSHKeys = nil },
				IsSet:       func() bool { return keysSet },
				Value:       func() any { return opts.SSHKeys },
			},
			{
				Name:        flagReplicas,
				Description: "Number of workers",
				Prompt:      wizard.TextInputPrompt,
				Default:     func(map[string]any) any { return strconv.Itoa(defaultPoolReplicas) },
				Validate:    func(v any) error { return validateReplicas(text(v)) },
				Setter:      func(v any) { opts.Replicas, _ = strconv.Atoi(text(v)) },
				Resetter:    func() { opts.Replicas = defaultPoolReplicas },
				IsSet:       func() bool { return replicasSet },
				Value:       func() any { return strconv.Itoa(opts.Replicas) },
			},
		},
	}
}

// clusterLoader lists the clusters, only those in -l when it is given.
func clusterLoader(locationFlag string, cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		clusters, err := cache.clusterList(ctx)
		if err != nil {
			return nil, err
		}
		var choices []wizard.Choice
		for i := range clusters {
			if locationFlag == "" || clusters[i].Location == locationFlag {
				choices = append(choices, wizard.Choice{Label: clusters[i].Name, Value: clusters[i].Name, Description: clusters[i].Location + ", " + clusters[i].Phase})
			}
		}
		if len(choices) == 0 {
			where := ""
			if locationFlag != "" {
				where = " in " + locationFlag
			}
			return nil, stopMessage("No cluster found" + where + ". Create one with `verda mk8s cluster create`.")
		}
		return choices, nil
	}
}

// instanceTypeRank orders types smallest first: CPU-only types, then by GPU
// count. The availability data carries no prices, so this stands in for cheapest first.
func instanceTypeRank(t string) int {
	if strings.HasPrefix(strings.ToUpper(t), "CPU") {
		return 0
	}
	n := 0
	for _, r := range t {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// instanceTypeLoader lists the types for the cluster's location, smallest
// first so Enter on the first row picks the cheapest, with no prices.
func instanceTypeLoader(opts *nodePoolCreateOptions, cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		types, err := cache.instanceTypes(ctx, opts.Location)
		if err != nil {
			return nil, err
		}
		if len(types) == 0 {
			return nil, stopMessage("No instance types are listed for " + opts.Location + ".")
		}
		sorted := slices.Clone(types)
		slices.SortStableFunc(sorted, func(a, b string) int { return cmp.Compare(instanceTypeRank(a), instanceTypeRank(b)) })
		choices := make([]wizard.Choice, len(sorted))
		for i, t := range sorted {
			choices[i] = wizard.Choice{Label: t, Value: t, Description: "instance types in " + opts.Location}
		}
		return choices, nil
	}
}

func sshKeyLoader(cache *wizardCache) wizard.LoaderFunc {
	return func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
		keys, err := cache.sshKeyList(ctx)
		if err != nil {
			return nil, err
		}
		if len(keys) == 0 {
			return nil, stopMessage("No SSH key found. Add one with `verda ssh-key add`.")
		}
		choices := make([]wizard.Choice, len(keys))
		for i := range keys {
			choices[i] = wizard.Choice{Label: keys[i].Name, Value: keys[i].ID, Description: keys[i].ID}
		}
		return choices, nil
	}
}
