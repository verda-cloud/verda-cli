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

// instanceBrief is an instance attached to a subnet, as shown by subnet describe.
type instanceBrief struct {
	ID        string `json:"id"                   yaml:"id"`
	Hostname  string `json:"hostname"             yaml:"hostname"`
	Status    string `json:"status"               yaml:"status"`
	PrivateIP string `json:"private_ip,omitempty" yaml:"private_ip,omitempty"`
	PublicIP  string `json:"public_ip,omitempty"  yaml:"public_ip,omitempty"`
}

// subnetDescribeView: Instances is nil (absent) when the instance list could not be read; an empty subnet has [].
type subnetDescribeView struct {
	cmdutil.SubnetView `yaml:",inline"`
	NetworkName        string           `json:"network_name"        yaml:"network_name"`
	Instances          *[]instanceBrief `json:"instances,omitempty" yaml:"instances,omitempty"`
}

// NewCmdSubnetDescribe creates the subnet describe command.
func NewCmdSubnetDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var networkArg string
	cmd := &cobra.Command{
		Use:     "describe [<subnet>]",
		Aliases: []string{"get", "show"},
		Short:   "Show a subnet, its default route and its instances",
		Example: cmdutil.Examples(`
			verda network subnet describe sn_82gp8jck
			verda network subnet describe fin-03-1 --network prod -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubnetDescribe(cmd, f, ioStreams, networkArg, args)
		},
	}
	cmd.Flags().StringVar(&networkArg, flagNetwork, "", "Network ID or name (needed when the subnet name is not unique)")
	return cmd
}

func runSubnetDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, networkArg string, args []string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := resolveSubnetArg(cmd, f, ioStreams, client, networkArg, args)
	if err != nil || subnet == nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response: subnet:", subnet)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	instances, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading instances...", func(ctx context.Context) ([]verda.PrivateNetworkWorkload, error) {
		return client.PrivateNetworks.GetSubnetWorkloads(ctx, network.ID, subnet.ID)
	})
	view := subnetDescribeView{SubnetView: cmdutil.NewSubnetView(network.ID, subnet), NetworkName: network.Name}
	if err != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not list the instances in this subnet: %v\n", err)
	} else {
		b := briefs(instances)
		view.Instances = &b
	}

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	renderSubnet(ioStreams.Out, &view)
	return nil
}

func briefs(instances []verda.PrivateNetworkWorkload) []instanceBrief {
	out := make([]instanceBrief, len(instances))
	for i := range instances {
		inst := &instances[i]
		b := instanceBrief{ID: inst.ID, Hostname: inst.Hostname, Status: inst.Status, PrivateIP: inst.PrivateIP}
		if inst.PublicIP != nil {
			b.PublicIP = *inst.PublicIP
		}
		out[i] = b
	}
	return out
}

func renderSubnet(out io.Writer, v *subnetDescribeView) {
	isDefault := yesNo(v.IsDefault)
	_, _ = fmt.Fprintf(out, "Name:       %s\n", v.Name)
	_, _ = fmt.Fprintf(out, "ID:         %s\n", v.ID)
	_, _ = fmt.Fprintf(out, "Network:    %s (%s)\n", v.NetworkName, v.NetworkID)
	_, _ = fmt.Fprintf(out, "Location:   %s\n", v.Location)
	_, _ = fmt.Fprintf(out, "CIDR:       %s\n", v.CIDR)
	_, _ = fmt.Fprintf(out, "Default:    %s\n", isDefault)
	_, _ = fmt.Fprintf(out, "IPs used:   %d/%d\n", v.UsedIPCount, v.TotalIPCount)
	_, _ = fmt.Fprintf(out, "Route:      %s\n", gatewayLabel(v.DefaultRouteInstance))
	_, _ = fmt.Fprintf(out, "Created:    %s\n", cmdutil.TimeColumn(v.CreatedAt, timeLayout))
	if v.Instances == nil {
		return
	}
	instances := *v.Instances
	_, _ = fmt.Fprintln(out)
	if len(instances) == 0 {
		_, _ = fmt.Fprintln(out, "No instances in this subnet.")
		return
	}
	_, _ = fmt.Fprintf(out, "Instances (%d):\n", len(instances))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HOSTNAME\tID\tSTATUS\tPRIVATE IP\tPUBLIC IP")
	for i := range instances {
		b := &instances[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", b.Hostname, b.ID, b.Status, cmdutil.TextColumn(b.PrivateIP), cmdutil.TextColumn(b.PublicIP))
	}
	_ = tw.Flush()
}

// gatewayLabel: "hostname (id, public ip)" for a route's instance, or "-".
func gatewayLabel(gw *cmdutil.AttachedInstanceView) string {
	if gw == nil {
		return noneMark
	}
	label := cmdutil.TextColumn(gw.Hostname) + " (" + gw.ID
	if gw.PublicIP != nil {
		label += ", " + *gw.PublicIP
	}
	return label + ")"
}
