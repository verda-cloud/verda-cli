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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdList creates the network list command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List private networks",
		Long: cmdutil.LongDesc(`
			List the private networks in your project. The default network
			is marked with * in the DEFAULT column.
		`),
		Example: cmdutil.Examples(`
			verda network list
			verda network ls -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, f, ioStreams)
		},
	}
}

func runList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	networks, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading private networks...", func(ctx context.Context) ([]verda.PrivateNetwork, error) {
		return client.PrivateNetworks.Get(ctx)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d private network(s):", len(networks)), networks)

	views := newNetworkViews(networks)
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		printNoNetworks(ioStreams)
		return nil
	}

	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tID\tMODE\tDEFAULT\tCREATED")
	for i := range views {
		v := &views[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.Name, v.ID, v.Mode, defaultColumn(v.IsDefault), cmdutil.TimeColumn(v.CreatedAt, timeLayout))
	}
	return tw.Flush()
}
