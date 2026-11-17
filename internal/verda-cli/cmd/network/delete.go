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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	conflictRetryHint = "If you just deleted these instances, wait a minute and try again."
)

// NewCmdDelete creates the network delete command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<network>]",
		Aliases: []string{"rm"},
		Short:   "Delete a private network with its subnets and routes",
		Long: cmdutil.LongDesc(`
			Delete a private network. Its subnets and routes are removed
			with it. The network must have no instances: delete them first
			(verda vm action --action delete). The CLI never deletes
			instances for you.

			Asks for confirmation; --yes skips it and is required in agent
			mode.
		`),
		Example: cmdutil.Examples(`
			verda network delete staging
			verda --agent network delete pn_d90ky5tk --yes
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
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, err := resolveNetworkArg(cmd, f, ioStreams, client, args)
	if err != nil || network == nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	stop := spinner(ctx, f, "Checking what the network contains...")
	instances, err := client.PrivateNetworks.GetWorkloads(ctx, network.ID)
	var subnets []verda.Subnet
	var routes map[string][]verda.Route
	if err == nil {
		subnets, routes, err = loadSubnetsAndRoutes(ctx, client, network.ID)
	}
	stop()
	cancel()
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		return instancesInUseError("Private network", "network", network.Name, network.ID, instances)
	}

	if !yes {
		ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
			"Delete private network "+network.Name+" ("+network.ID+")",
			networkDeleteDetail(subnets, routes),
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
	ctx, cancel = context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting private network...", func(ctx context.Context) error {
		return client.PrivateNetworks.Delete(ctx, network.ID)
	})
	if err != nil {
		return withConflictHint(err)
	}
	return writeDeleted(ioStreams, f, "network", network.ID, network.Name)
}

func loadSubnetsAndRoutes(ctx context.Context, client *verda.Client, networkID string) ([]verda.Subnet, map[string][]verda.Route, error) {
	subnets, err := client.PrivateNetworks.GetSubnets(ctx, networkID)
	if err != nil {
		return nil, nil, err
	}
	routes := make(map[string][]verda.Route, len(subnets))
	for i := range subnets {
		r, err := client.PrivateNetworks.GetRoutes(ctx, networkID, subnets[i].ID)
		if err != nil {
			return nil, nil, err
		}
		routes[subnets[i].ID] = r
	}
	return subnets, routes, nil
}

func networkDeleteDetail(subnets []verda.Subnet, routes map[string][]verda.Route) []string {
	if len(subnets) == 0 {
		return []string{"The network has no subnets."}
	}
	lines := []string{fmt.Sprintf("Also removes %d subnet(s):", len(subnets))}
	for i := range subnets {
		s := &subnets[i]
		lines = append(lines, fmt.Sprintf("  - %s (%s, %s, %s)", s.Name, s.ID, s.Location.Code, s.CIDR))
		for j := range routes[s.ID] {
			lines = append(lines, "      route "+routeSummary(&routes[s.ID][j]))
		}
	}
	return lines
}

func routeSummary(r *verda.Route) string {
	return r.Destination + " via " + gatewayName(r.Instance)
}

func gatewayName(a *verda.AttachedInstance) string {
	if a == nil || a.ID == "" {
		return deletedMark
	}
	if a.Hostname == "" {
		return a.ID
	}
	return a.Hostname
}

// instancesInUseError refuses a delete while instances are attached; the API would answer 409.
func instancesInUseError(title, resource, name, id string, instances []verda.PrivateNetworkWorkload) *cmdutil.AgentError {
	labels := make([]string, len(instances))
	details := make([]map[string]string, len(instances))
	for i := range instances {
		inst := &instances[i]
		labels[i] = fmt.Sprintf("%s (%s, %s, %s)", inst.Hostname, inst.ID, inst.Location.Code, cmdutil.TextColumn(inst.PrivateIP))
		details[i] = map[string]string{"id": inst.ID, "hostname": inst.Hostname, "location": inst.Location.Code, "private_ip": inst.PrivateIP}
	}
	return cmdutil.NewResourceInUseError(resource, id,
		fmt.Sprintf("%s %s has %d instance(s): %s. Delete them first (verda vm action --action delete), then run this again.",
			title, name, len(instances), strings.Join(labels, ", ")),
		details)
}

// withConflictHint adds the retry hint to an API 409 conflict; the error code is unchanged.
func withConflictHint(err error) error {
	var apiErr *verda.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		return err
	}
	ae := cmdutil.ClassifyError(err)
	ae.Message += "\n" + conflictRetryHint
	return ae
}

func writeDeleted(ioStreams cmdutil.IOStreams, f cmdutil.Factory, resource, id, name string) error {
	result := map[string]string{"resource": resource, "id": id, "name": name, "action": "delete", "status": "completed"}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %s %s (%s)\n", resource, name, id)
	return nil
}

func warnStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
}

// confirmDestructive prints a red warning with detail lines, then asks.
// (false, nil) on decline or a clean Esc/Ctrl+C; agent callers enforce --yes themselves.
func confirmDestructive(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, heading string, detail []string, prompt string) (bool, error) {
	warn := warnStyle()
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
