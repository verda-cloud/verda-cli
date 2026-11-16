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

const (
	flagLocation = "location"
	flagSubnet   = "subnet"
	argSubnet    = "<subnet>"
)

// NewCmdSubnet creates the `network subnet` parent command.
func NewCmdSubnet(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "subnet",
		Aliases: []string{"subnets"},
		Short:   "Manage the subnets of private networks",
		Long: cmdutil.LongDesc(`
			Manage subnets. A subnet belongs to one network and one location;
			the first subnet in a location becomes its default. A subnet is
			an ID (sn_..., unique everywhere) or a name; a name may need
			--network when several networks use it.
		`),
		Example: cmdutil.Examples(`
			verda network subnet list
			verda network subnet create --network prod -l FIN-03
			verda network subnet describe sn_82gp8jck
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdSubnetList(f, ioStreams),
		NewCmdSubnetDescribe(f, ioStreams),
		NewCmdSubnetCreate(f, ioStreams),
		NewCmdSubnetUpdate(f, ioStreams),
		NewCmdSubnetDelete(f, ioStreams),
	)
	return cmd
}

// resolveSubnetArg: args[0] by ID or name; else a picker on a TTY; else a missing-arg error.
// A nil subnet with a nil error means the user canceled the picker.
func resolveSubnetArg(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, networkArg string, args []string) (*verda.PrivateNetwork, *verda.Subnet, error) {
	if len(args) > 0 {
		ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
		defer cancel()
		stop := spinner(ctx, f, "Loading subnets...")
		n, s, err := cmdutil.ResolveSubnet(ctx, client, networkArg, args[0])
		stop()
		return n, s, err
	}
	if !interactiveTTY(f) {
		return nil, nil, cmdutil.NewMissingFlagsError([]string{argSubnet})
	}
	return selectSubnet(cmd.Context(), f, ioStreams, client, networkArg)
}

// subnetRow is one subnet with the network it was read from.
type subnetRow struct {
	network *verda.PrivateNetwork
	subnet  verda.Subnet
}

// loadSubnetRows reads the subnets of one network (networkArg) or of all networks.
func loadSubnetRows(ctx context.Context, client *verda.Client, networkArg string) ([]subnetRow, error) {
	var networks []verda.PrivateNetwork
	if networkArg != "" {
		n, err := cmdutil.ResolveNetwork(ctx, client, networkArg)
		if err != nil {
			return nil, err
		}
		networks = []verda.PrivateNetwork{*n}
	} else {
		all, err := client.PrivateNetworks.Get(ctx)
		if err != nil {
			return nil, err
		}
		networks = all
	}
	var rows []subnetRow
	for i := range networks {
		subnets, err := client.PrivateNetworks.GetSubnets(ctx, networks[i].ID)
		if err != nil {
			return nil, err
		}
		for j := range subnets {
			rows = append(rows, subnetRow{network: &networks[i], subnet: subnets[j]})
		}
	}
	return rows, nil
}

// selectSubnet prompts for a subnet; cancel or an empty list → nil, nil, nil.
func selectSubnet(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, networkArg string) (*verda.PrivateNetwork, *verda.Subnet, error) {
	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	rows, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading subnets...", func(ctx context.Context) ([]subnetRow, error) {
		return loadSubnetRows(ctx, client, networkArg)
	})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No subnets found.")
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda network subnet create")
		return nil, nil, nil
	}
	labels := make([]string, 0, len(rows)+1)
	for i := range rows {
		s := &rows[i].subnet
		labels = append(labels, fmt.Sprintf("%s / %s  (%s, %s, %s)", rows[i].network.Name, s.Name, s.ID, s.Location.Code, s.CIDR))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select subnet", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if idx < 0 || idx >= len(rows) {
		return nil, nil, nil
	}
	return rows[idx].network, &rows[idx].subnet, nil
}
