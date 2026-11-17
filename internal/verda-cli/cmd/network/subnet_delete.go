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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSubnetDelete creates the subnet delete command.
func NewCmdSubnetDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var networkArg string
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<subnet-id>]",
		Aliases: []string{"rm"},
		Short:   "Delete a subnet",
		Long: cmdutil.LongDesc(`
			Delete a subnet. The subnet must have no instances: delete them
			first (verda vm action --action delete). The CLI never deletes
			instances for you. A subnet ID needs --network. On a terminal,
			with no subnet ID, the CLI shows a picker (of one network with
			--network).

			Asks for confirmation. --yes skips the confirmation and is
			required in agent mode.
		`),
		Example: cmdutil.Examples(`
			verda network subnet delete $SUBNET_ID --network $NETWORK_ID
			verda --agent network subnet delete $SUBNET_ID --network $NETWORK_ID --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubnetDelete(cmd, f, ioStreams, networkArg, args, yes)
		},
	}
	cmd.Flags().StringVar(&networkArg, flagNetwork, "", "Network ID (required with a subnet ID)")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runSubnetDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, networkArg string, args []string, yes bool) error {
	if f.AgentMode() && !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	if err := checkSubnetArgs(f, networkArg, args); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	networkID, subnet, err := resolveSubnetArg(cmd, f, ioStreams, client, networkArg, args)
	if err != nil || subnet == nil {
		return err
	}

	if !yes {
		ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Delete subnet %s (%s, %s, %s) of network %s", subnet.Name, subnet.ID, subnet.Location.Code, subnet.CIDR, networkID),
			nil,
			"Delete subnet "+subnet.Name+"?")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete subnet:", map[string]string{"network_id": networkID, "id": subnet.ID, "name": subnet.Name})
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting subnet...", func(ctx context.Context) error {
		return client.PrivateNetworks.DeleteSubnet(ctx, networkID, subnet.ID)
	})
	if err != nil {
		return err
	}
	return writeDeleted(ioStreams, f, "subnet", subnet.ID, subnet.Name)
}
