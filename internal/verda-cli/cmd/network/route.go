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
	"context"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdRoute creates the `network route` parent command.
func NewCmdRoute(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "route",
		Aliases: []string{"routes"},
		Short:   "Manage the routes of a subnet (NAT gateways)",
		Long: cmdutil.LongDesc(`
			A route sends a subnet's traffic for one destination (today only
			0.0.0.0/0, the internet) through one of your instances. That
			instance must be on the same network, in the subnet's location,
			have a public IP, and do IP forwarding and NAT itself.

			Every route command takes --subnet (an ID, or a name with
			--network when the name is not unique).
		`),
		Example: cmdutil.Examples(`
			verda network route list --subnet sn_82gp8jck
			verda network route create --subnet sn_82gp8jck --instance nat-1
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdRouteList(f, ioStreams),
		NewCmdRouteCreate(f, ioStreams),
		NewCmdRouteDelete(f, ioStreams),
	)
	return cmd
}

// routeTarget is the --network/--subnet pair every route command takes.
type routeTarget struct {
	network string
	subnet  string
}

func (t *routeTarget) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.subnet, flagSubnet, "", "Subnet ID or name")
	cmd.Flags().StringVar(&t.network, flagNetwork, "", "Network ID or name (needed when the subnet name is not unique)")
}

// resolve finds the subnet; on a TTY a missing --subnet opens a picker. nil, nil, nil = canceled.
func (t *routeTarget) resolve(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client) (*verda.PrivateNetwork, *verda.Subnet, error) {
	if t.subnet == "" && !interactiveTTY(f) {
		return nil, nil, cmdutil.NewMissingFlagsError([]string{"--" + flagSubnet})
	}
	var args []string
	if t.subnet != "" {
		args = []string{t.subnet}
	}
	return resolveSubnetArg(cmd, f, ioStreams, client, t.network, args)
}

// instanceIndex maps instance ID → instance for live (not discontinued) instances.
func instanceIndex(instances []verda.Instance) map[string]*verda.Instance {
	idx := make(map[string]*verda.Instance, len(instances))
	for i := range instances {
		if instances[i].Status != verda.StatusDiscontinued {
			idx[instances[i].ID] = &instances[i]
		}
	}
	return idx
}

// loadRoutes reads a subnet's routes and, best effort, the instance list (nil when it failed).
func loadRoutes(ctx context.Context, client *verda.Client, networkID, subnetID string) ([]verda.Route, map[string]*verda.Instance, error) {
	routes, err := client.PrivateNetworks.GetRoutes(ctx, networkID, subnetID)
	if err != nil {
		return nil, nil, err
	}
	instances, err := client.Instances.List(ctx, nil)
	if err != nil {
		return routes, nil, nil //nolint:nilerr // best effort: without the list, gateways are shown as the API reports them
	}
	return routes, instanceIndex(instances), nil
}

// gatewayGone: the route has no instance, or the instance is no longer live.
// With an unknown instance list (nil index) only a missing instance counts.
func gatewayGone(r *verda.Route, live map[string]*verda.Instance) bool {
	if r.Instance == nil || r.Instance.ID == "" {
		return true
	}
	if live == nil {
		return false
	}
	_, ok := live[r.Instance.ID]
	return !ok
}
