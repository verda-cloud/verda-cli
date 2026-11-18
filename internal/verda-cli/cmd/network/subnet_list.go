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
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const noneMark = "-"

// NewCmdSubnetList creates the subnet list command.
func NewCmdSubnetList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var networkArg, location string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List subnets of all networks, or of one",
		Example: cmdutil.Examples(`
			verda network subnet list
			verda network subnet list --network $NETWORK_ID -l FIN-03
			verda network subnet ls -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSubnetList(cmd, f, ioStreams, networkArg, location)
		},
	}
	cmd.Flags().StringVar(&networkArg, flagNetwork, "", "Network ID (default: all networks)")
	cmd.Flags().StringVarP(&location, flagLocation, "l", "", "Only subnets in this location, for example FIN-03")
	return cmd
}

func runSubnetList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, networkArg, location string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	rows, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading subnets...", func(ctx context.Context) ([]subnetRow, error) {
		return loadSubnetRows(ctx, client, networkArg)
	})
	if err != nil {
		return err
	}

	if f.Debug() {
		subnets := make([]verda.Subnet, 0, len(rows))
		for i := range rows {
			subnets = append(subnets, rows[i].subnet)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, true, fmt.Sprintf("API response: %d subnet(s), before the location filter:", len(subnets)), subnets)
	}

	views := make([]subnetView, 0, len(rows))
	names := map[string]string{}
	for i := range rows {
		if location != "" && !strings.EqualFold(rows[i].subnet.Location.Code, location) {
			continue
		}
		views = append(views, newSubnetView(rows[i].network.ID, &rows[i].subnet))
		names[rows[i].network.ID] = rows[i].network.Name
	}

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		printNoSubnets(ioStreams)
		return nil
	}

	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NETWORK\tNAME\tID\tLOCATION\tCIDR\tDEFAULT\tIPS USED/TOTAL\tROUTE")
	for i := range views {
		s := &views[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d/%d\t%s\n",
			names[s.NetworkID], s.Name, s.ID, s.Location, s.CIDR, defaultColumn(s.IsDefault), s.UsedIPCount, s.TotalIPCount, routeColumn(s.DefaultRouteInstance))
	}
	return tw.Flush()
}

func routeColumn(gw *attachedInstanceView) string {
	if gw == nil {
		return noneMark
	}
	return cmdutil.TextColumn(gw.Hostname)
}
