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
	"slices"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// wizardCache memoizes the lookups the create wizards share, so going back to
// a step or filling a second step from the same list costs no extra call.
type wizardCache struct {
	f      cmdutil.Factory
	client *verda.Client

	locations    []string
	haveLocation bool
	networks     []verda.PrivateNetwork
	haveNetworks bool
	subnets      map[string][]verda.Subnet

	clusters     []verda.ManagedKubernetesCluster
	haveClusters bool
	types        map[string][]string
	sshKeys      []verda.SSHKey
	haveKeys     bool
}

func newWizardCache(f cmdutil.Factory, client *verda.Client) *wizardCache {
	return &wizardCache{f: f, client: client, subnets: map[string][]verda.Subnet{}, types: map[string][]string{}}
}

func (c *wizardCache) enabledLocations(ctx context.Context) ([]string, error) {
	if c.haveLocation {
		return c.locations, nil
	}
	res, err := apiCall(ctx, c.f, "Loading locations...", func(ctx context.Context) (*verda.ManagedKubernetesEnabledLocations, error) {
		return c.client.ManagedKubernetes.GetEnabledLocations(ctx)
	})
	if err != nil {
		return nil, err
	}
	c.locations, c.haveLocation = res.Locations, true
	return c.locations, nil
}

func (c *wizardCache) privateNetworks(ctx context.Context) ([]verda.PrivateNetwork, error) {
	if c.haveNetworks {
		return c.networks, nil
	}
	networks, err := apiCall(ctx, c.f, "Loading private networks...", func(ctx context.Context) ([]verda.PrivateNetwork, error) {
		return c.client.PrivateNetworks.Get(ctx)
	})
	if err != nil {
		return nil, err
	}
	c.networks, c.haveNetworks = networks, true
	return c.networks, nil
}

func (c *wizardCache) networkSubnets(ctx context.Context, networkID string) ([]verda.Subnet, error) {
	if s, ok := c.subnets[networkID]; ok {
		return s, nil
	}
	subnets, err := apiCall(ctx, c.f, "Loading subnets...", func(ctx context.Context) ([]verda.Subnet, error) {
		return c.client.PrivateNetworks.GetSubnets(ctx, networkID)
	})
	if err != nil {
		return nil, err
	}
	c.subnets[networkID] = subnets
	return subnets, nil
}

// networkName: the name of a network the wizard has listed, else the ID.
func (c *wizardCache) networkName(id string) string {
	for i := range c.networks {
		if c.networks[i].ID == id {
			return c.networks[i].Name
		}
	}
	return id
}

// stopError ends a wizard before any create call; runWizard shows its text as written.
type stopError struct{ msg string }

func (e *stopError) Error() string { return e.msg }

func stopMessage(msg string) error { return &stopError{msg: msg} }

func (c *wizardCache) clusterList(ctx context.Context) ([]verda.ManagedKubernetesCluster, error) {
	if c.haveClusters {
		return c.clusters, nil
	}
	clusters, err := listClusters(ctx, c.f, c.client)
	if err != nil {
		return nil, err
	}
	c.clusters, c.haveClusters = clusters, true
	return c.clusters, nil
}

func (c *wizardCache) clusterLocation(name string) string {
	for i := range c.clusters {
		if c.clusters[i].Name == name {
			return c.clusters[i].Location
		}
	}
	return ""
}

// instanceTypes lists the types the availability API reports for a location.
// It ignores private-network capacity, so a listed type can still be refused.
func (c *wizardCache) instanceTypes(ctx context.Context, location string) ([]string, error) {
	if t, ok := c.types[location]; ok {
		return t, nil
	}
	avail, err := apiCall(ctx, c.f, "Loading instance types...", func(ctx context.Context) ([]verda.LocationAvailability, error) {
		return c.client.InstanceAvailability.GetAllAvailabilities(ctx, false, location)
	})
	if err != nil {
		return nil, err
	}
	var types []string
	for i := range avail {
		if avail[i].LocationCode == location {
			types = append(types, avail[i].Availabilities...)
		}
	}
	slices.Sort(types)
	types = slices.Compact(types)
	c.types[location] = types
	return types, nil
}

func (c *wizardCache) sshKeyList(ctx context.Context) ([]verda.SSHKey, error) {
	if c.haveKeys {
		return c.sshKeys, nil
	}
	keys, err := apiCall(ctx, c.f, "Loading SSH keys...", func(ctx context.Context) ([]verda.SSHKey, error) {
		return c.client.SSHKeys.GetAllSSHKeys(ctx)
	})
	if err != nil {
		return nil, err
	}
	c.sshKeys, c.haveKeys = keys, true
	return c.sshKeys, nil
}
