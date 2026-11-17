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
	flagNetwork  = "network"
	flagLocation = "location"
	argSubnet    = "<subnet-id>"
)

// NewCmdSubnet creates the `network subnet` parent command.
func NewCmdSubnet(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "subnet",
		Aliases: []string{"subnets"},
		Short:   "Manage the subnets of private networks",
		Long: cmdutil.LongDesc(`
			Manage subnets. A subnet belongs to one network and one location.
			Commands take IDs, not names.
		`),
		Example: cmdutil.Examples(`
			verda network subnet list
			verda network subnet create --network $NETWORK_ID -l FIN-03
			verda network subnet delete $SUBNET_ID --network $NETWORK_ID
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdSubnetList(f, ioStreams),
		NewCmdSubnetCreate(f, ioStreams),
		NewCmdSubnetDelete(f, ioStreams),
	)
	return cmd
}

// checkSubnetArgs: a subnet ID needs --network; off a TTY both are required.
// Call it before f.VerdaClient() so bad input fails the same with or without credentials.
func checkSubnetArgs(f cmdutil.Factory, networkID string, args []string) error {
	if len(args) == 0 && interactiveTTY(f) {
		return nil
	}
	var missing []string
	if len(args) == 0 {
		missing = append(missing, argSubnet)
	}
	if networkID == "" {
		missing = append(missing, "--"+flagNetwork)
	}
	if len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	return nil
}

// resolveSubnetArg returns the network ID and the subnet: args[0] with --network,
// else a picker (of one network with --network). Call checkSubnetArgs first.
// A nil subnet with a nil error means the user canceled the picker.
func resolveSubnetArg(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, networkID string, args []string) (string, *verda.Subnet, error) {
	if len(args) == 0 {
		return selectSubnet(cmd.Context(), f, ioStreams, client, networkID)
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	subnet, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading subnet...", func(ctx context.Context) (*verda.Subnet, error) {
		return client.PrivateNetworks.GetSubnetByID(ctx, networkID, args[0])
	})
	return networkID, subnet, err
}

// subnetRow is one subnet with the network it was read from.
type subnetRow struct {
	network *verda.PrivateNetwork
	subnet  verda.Subnet
}

// loadSubnetRows reads the subnets of one network (networkID) or of all networks.
func loadSubnetRows(ctx context.Context, client *verda.Client, networkID string) ([]subnetRow, error) {
	networks, err := client.PrivateNetworks.Get(ctx)
	if err != nil {
		return nil, err
	}
	if networkID != "" {
		n := networkByID(networks, networkID)
		if n == nil {
			return nil, cmdutil.NewNotFoundError("network", networkID)
		}
		networks = []verda.PrivateNetwork{*n}
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

// selectSubnet prompts for a subnet; cancel or an empty list → "", nil, nil.
func selectSubnet(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, networkID string) (string, *verda.Subnet, error) {
	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()
	rows, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading subnets...", func(ctx context.Context) ([]subnetRow, error) {
		return loadSubnetRows(ctx, client, networkID)
	})
	if err != nil {
		return "", nil, err
	}
	if len(rows) == 0 {
		printNoSubnets(ioStreams)
		return "", nil, nil
	}
	labels := make([]string, 0, len(rows)+1)
	for i := range rows {
		s := &rows[i].subnet
		labels = append(labels, fmt.Sprintf("%s / %s  (%s, %s, %s)", rows[i].network.Name, s.Name, s.ID, s.Location.Code, s.CIDR))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select subnet", labels, tui.WithShowHints(true))
	if err != nil && !cmdutil.IsPromptCancel(err) {
		return "", nil, err
	}
	if err != nil || idx < 0 || idx >= len(rows) {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
		return "", nil, nil
	}
	return rows[idx].network.ID, &rows[idx].subnet, nil
}

func printNoSubnets(ioStreams cmdutil.IOStreams) {
	_, _ = fmt.Fprintln(ioStreams.Out, "No subnets found.")
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda network subnet create")
}
