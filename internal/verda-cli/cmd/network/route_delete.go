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
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const argRouteID = "<route-id>"

// NewCmdRouteDelete creates the route delete command.
func NewCmdRouteDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	target := &routeTarget{}
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<route-id>]",
		Aliases: []string{"rm"},
		Short:   "Delete a route of a subnet",
		Long: cmdutil.LongDesc(`
			Delete a route. Instances in the subnet lose internet access
			through the route's gateway instance.

			Asks for confirmation; --yes skips it and is required in agent
			mode.
		`),
		Example: cmdutil.Examples(`
			verda network route delete rt_82gp8jck --subnet sn_82gp8jck
			verda --agent network route delete rt_82gp8jck --subnet sn_82gp8jck --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRouteDelete(cmd, f, ioStreams, target, args, yes)
		},
	}
	target.addFlags(cmd)
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runRouteDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, target *routeTarget, args []string, yes bool) error {
	if f.AgentMode() && !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	if len(args) == 0 && !interactiveTTY(f) {
		missing := []string{argRouteID}
		if target.subnet == "" {
			missing = append(missing, "--"+flagSubnet)
		}
		return cmdutil.NewMissingFlagsError(missing)
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := target.resolve(cmd, f, ioStreams, client)
	if err != nil || subnet == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	stop := spinner(ctx, f, "Loading routes...")
	routes, live, err := loadRoutes(ctx, client, ioStreams.ErrOut, network.ID, subnet.ID)
	stop()
	cancel()
	if err != nil {
		return err
	}
	route, err := pickRoute(cmd.Context(), f, ioStreams, routes, subnet, args)
	if err != nil || route == nil {
		return err
	}

	if !yes {
		gw := gatewayName(route.Instance)
		if gatewayGone(route, live) {
			gw = "a deleted instance"
		}
		ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Delete route %s (%s) of subnet %s", route.ID, route.Destination, subnet.Name),
			[]string{fmt.Sprintf("Instances in %s lose internet access through %s.", subnet.Name, gw)},
			"Delete route "+route.ID+"?")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete route:", map[string]string{"network_id": network.ID, "subnet_id": subnet.ID, "id": route.ID})
	ctx, cancel = context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting route...", func(ctx context.Context) error {
		return client.PrivateNetworks.DeleteRoute(ctx, network.ID, subnet.ID, route.ID)
	})
	if err != nil {
		return err
	}
	return writeDeleted(ioStreams, f, "route", route.ID, route.Destination)
}

// pickRoute: args[0] must be a route of the subnet; else a picker. nil, nil = canceled or none.
func pickRoute(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, routes []verda.Route, subnet *verda.Subnet, args []string) (*verda.Route, error) {
	if len(args) > 0 {
		for i := range routes {
			if routes[i].ID == args[0] {
				return &routes[i], nil
			}
		}
		return nil, cmdutil.NewNotFoundError("route", args[0])
	}
	if len(routes) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "No routes in subnet %s.\n", subnet.Name)
		return nil, nil
	}
	labels := make([]string, 0, len(routes)+1)
	for i := range routes {
		labels = append(labels, routes[i].ID+"  "+routeSummary(&routes[i]))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select route", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	if idx < 0 || idx >= len(routes) {
		return nil, nil
	}
	return &routes[idx], nil
}
