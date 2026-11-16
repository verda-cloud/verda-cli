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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSubnetDelete creates the subnet delete command.
func NewCmdSubnetDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var networkArg string
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<subnet>]",
		Aliases: []string{"rm"},
		Short:   "Delete a subnet and its routes",
		Long: cmdutil.LongDesc(`
			Delete a subnet. Its routes are removed with it. The subnet must
			have no instances: delete them first (verda vm action --action
			delete). The CLI never deletes instances for you.

			Asks for confirmation; --yes skips it and is required in agent
			mode.
		`),
		Example: cmdutil.Examples(`
			verda network subnet delete sn_82gp8jck
			verda --agent network subnet delete web --network prod --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubnetDelete(cmd, f, ioStreams, networkArg, args, yes)
		},
	}
	cmd.Flags().StringVar(&networkArg, flagNetwork, "", "Network ID or name (needed when the subnet name is not unique)")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runSubnetDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, networkArg string, args []string, yes bool) error {
	if f.AgentMode() && !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := resolveSubnetArg(cmd, f, ioStreams, client, networkArg, args)
	if err != nil || subnet == nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	stop := spinner(ctx, f, "Checking what the subnet contains...")
	instances, err := cmdutil.ListInstancesOn(ctx, client, network.ID, subnet.ID)
	var routes []verda.Route
	if err == nil {
		routes, err = client.PrivateNetworks.GetRoutes(ctx, network.ID, subnet.ID)
	}
	stop()
	cancel()
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		return instancesInUseError("Subnet", "subnet", subnet.Name, subnet.ID, instances)
	}

	if !yes {
		ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Delete subnet %s (%s, %s, %s) of network %s", subnet.Name, subnet.ID, subnet.Location.Code, subnet.CIDR, network.Name),
			subnetDeleteDetail(routes),
			"Delete subnet "+subnet.Name+"?")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete subnet:", map[string]string{"network_id": network.ID, "id": subnet.ID, "name": subnet.Name})
	ctx, cancel = context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting subnet...", func(ctx context.Context) error {
		return client.PrivateNetworks.DeleteSubnet(ctx, network.ID, subnet.ID)
	})
	if err != nil {
		return withConflictHint(err)
	}
	return writeDeleted(ioStreams, f, "subnet", subnet.ID, subnet.Name)
}

func subnetDeleteDetail(routes []verda.Route) []string {
	if len(routes) == 0 {
		return []string{"The subnet has no routes."}
	}
	lines := []string{fmt.Sprintf("Also removes %d route(s):", len(routes))}
	for i := range routes {
		lines = append(lines, "  - "+routes[i].ID+": "+routeSummary(&routes[i]))
	}
	return lines
}
