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
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// networkDescribeView is the structured output of network describe.
// InstanceCount is absent when the instance list could not be read.
type networkDescribeView struct {
	cmdutil.NetworkView `yaml:",inline"`
	Subnets             []cmdutil.SubnetView `json:"subnets"                  yaml:"subnets"`
	InstanceCount       *int                 `json:"instance_count,omitempty" yaml:"instance_count,omitempty"`
}

// NewCmdDescribe creates the network describe command.
func NewCmdDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "describe [<network>]",
		Aliases: []string{"get", "show"},
		Short:   "Show a private network with its subnets",
		Long: cmdutil.LongDesc(`
			Show a private network, its subnets (with their default route)
			and the number of instances attached to it. The network is an
			ID or a name; on a terminal, omit it to pick from a list.
		`),
		Example: cmdutil.Examples(`
			verda network describe prod
			verda network describe pn_d90ky5tk -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(cmd, f, ioStreams, args)
		},
	}
}

func runDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, err := resolveNetworkArg(cmd, f, ioStreams, client, args)
	if err != nil || network == nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	stop := spinner(ctx, f, "Loading subnets...")
	subnets, err := client.PrivateNetworks.GetSubnets(ctx, network.ID)
	var instances []verda.Instance
	var instErr error
	if err == nil {
		instances, instErr = cmdutil.ListInstancesOn(ctx, client, network.ID, "")
	}
	stop()
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response: private network:", network)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d subnet(s):", len(subnets)), subnets)

	view := networkDescribeView{
		NetworkView: cmdutil.NewNetworkView(network),
		Subnets:     cmdutil.NewSubnetViews(network.ID, subnets),
	}
	if instErr == nil {
		n := len(instances)
		view.InstanceCount = &n
	} else {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not count the instances on this network: %v\n", instErr)
	}

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	return renderNetwork(ioStreams.Out, &view)
}

func renderNetwork(out io.Writer, v *networkDescribeView) error {
	isDefault := yesNo(v.IsDefault)
	_, _ = fmt.Fprintf(out, "Name:       %s\n", v.Name)
	_, _ = fmt.Fprintf(out, "ID:         %s\n", v.ID)
	_, _ = fmt.Fprintf(out, "Mode:       %s\n", v.Mode)
	_, _ = fmt.Fprintf(out, "Default:    %s\n", isDefault)
	_, _ = fmt.Fprintf(out, "Created:    %s\n", cmdutil.TimeColumn(v.CreatedAt, timeLayout))
	if v.InstanceCount != nil {
		_, _ = fmt.Fprintf(out, "Instances:  %d\n", *v.InstanceCount)
	}
	_, _ = fmt.Fprintln(out)
	if len(v.Subnets) == 0 {
		_, _ = fmt.Fprintln(out, "No subnets. Create one with: verda network subnet create --network "+v.Name)
		return nil
	}
	_, _ = fmt.Fprintln(out, "Subnets:")
	return writeSubnetTable(out, v.Subnets, nil)
}

// writeSubnetTable prints subnets; non-nil networkNames (ID → name) adds a NETWORK column.
func writeSubnetTable(out io.Writer, subnets []cmdutil.SubnetView, networkNames map[string]string) error {
	withNetwork := networkNames != nil
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	header := "NAME\tID\tLOCATION\tCIDR\tDEFAULT\tIPS USED/TOTAL\tROUTE"
	if withNetwork {
		header = "NETWORK\t" + header
	}
	_, _ = fmt.Fprintln(tw, header)
	for i := range subnets {
		s := &subnets[i]
		if withNetwork {
			_, _ = fmt.Fprintf(tw, "%s\t", networkNames[s.NetworkID])
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d/%d\t%s\n",
			s.Name, s.ID, s.Location, s.CIDR, defaultColumn(s.IsDefault), s.UsedIPCount, s.TotalIPCount, routeColumn(s.DefaultRouteInstance))
	}
	return tw.Flush()
}

func routeColumn(gw *cmdutil.AttachedInstanceView) string {
	if gw == nil {
		return noneMark
	}
	return cmdutil.TextColumn(gw.Hostname)
}
