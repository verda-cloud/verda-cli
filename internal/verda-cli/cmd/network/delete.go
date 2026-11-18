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

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdDelete creates the network delete command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<network-id>]",
		Aliases: []string{"rm"},
		Short:   "Delete a private network with its subnets",
		Long: cmdutil.LongDesc(`
			Delete a private network. Its subnets are removed with it. The
			network must have no instances: delete them first (verda vm
			action --action delete). The CLI never deletes instances for you.

			Asks for confirmation. --yes skips the confirmation and is
			required in agent mode.
		`),
		Example: cmdutil.Examples(`
			verda network delete $NETWORK_ID
			verda --agent network delete $NETWORK_ID --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, ioStreams, args, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string, yes bool) error {
	if f.AgentMode() && !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	if err := checkNetworkArg(f, args); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, err := resolveNetworkArg(cmd, f, ioStreams, client, args)
	if err != nil || network == nil {
		return err
	}

	if !yes {
		ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
		subnets, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading subnets...", func(ctx context.Context) ([]verda.Subnet, error) {
			return client.PrivateNetworks.GetSubnets(ctx, network.ID)
		})
		cancel()
		if err != nil {
			return err
		}
		ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
			"Delete private network "+network.Name+" ("+network.ID+")",
			networkDeleteDetail(subnets),
			"Delete network "+network.Name+"?")
		if err != nil {
			return err
		}
		if !ok {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete private network:", map[string]string{"id": network.ID, "name": network.Name})
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting private network...", func(ctx context.Context) error {
		return client.PrivateNetworks.Delete(ctx, network.ID)
	})
	if err != nil {
		return err
	}
	return writeDeleted(ioStreams, f, "network", network.ID, network.Name)
}

func networkDeleteDetail(subnets []verda.Subnet) []string {
	if len(subnets) == 0 {
		return []string{"The network has no subnets."}
	}
	lines := []string{fmt.Sprintf("Also removes %d subnet(s):", len(subnets))}
	for i := range subnets {
		s := &subnets[i]
		lines = append(lines, fmt.Sprintf("  - %s (%s, %s, %s)", s.Name, s.ID, s.Location.Code, s.CIDR))
	}
	return lines
}

func writeDeleted(ioStreams cmdutil.IOStreams, f cmdutil.Factory, resource, id, name string) error {
	result := map[string]string{"resource": resource, "id": id, "name": name, "action": "delete", "status": "completed"}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %s %s (%s)\n", resource, name, id)
	return nil
}

// confirmDestructive prints a red warning with detail lines, then asks.
// (false, nil) on decline or a clean Esc/Ctrl+C; agent callers enforce --yes themselves.
func confirmDestructive(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, heading string, detail []string, prompt string) (bool, error) {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s %s\n", warn.Render("⚠"), warn.Render(heading))
	for _, line := range detail {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n", line)
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", warn.Render("This action cannot be undone."))
	confirmed, err := prompter.Confirm(ctx, prompt)
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	return confirmed, nil
}
