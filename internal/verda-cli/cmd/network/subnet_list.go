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

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSubnetList creates the subnet list command.
func NewCmdSubnetList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var networkArg, location string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List subnets of all networks, or of one",
		Example: cmdutil.Examples(`
			verda network subnet list
			verda network subnet list --network prod -l FIN-03
			verda network subnet ls -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSubnetList(cmd, f, ioStreams, networkArg, location)
		},
	}
	cmd.Flags().StringVar(&networkArg, flagNetwork, "", "Network ID or name (default: all networks)")
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

	views := make([]cmdutil.SubnetView, 0, len(rows))
	names := map[string]string{}
	for i := range rows {
		if location != "" && !strings.EqualFold(rows[i].subnet.Location.Code, location) {
			continue
		}
		views = append(views, cmdutil.NewSubnetView(rows[i].network.ID, &rows[i].subnet))
		names[rows[i].network.ID] = rows[i].network.Name
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d subnet(s):", len(views)), views)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No subnets found.")
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda network subnet create")
		return nil
	}
	return writeSubnetTable(ioStreams.Out, views, names)
}
