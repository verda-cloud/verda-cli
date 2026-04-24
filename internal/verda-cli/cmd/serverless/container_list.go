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

package serverless

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdContainerList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List serverless container deployments",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContainerList(cmd, f, ioStreams)
		},
	}
	return cmd
}

func runContainerList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	deployments, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading container deployments...", func() ([]verda.ContainerDeployment, error) {
		return client.ContainerDeployments.GetDeployments(ctx)
	})
	if err != nil {
		return fmt.Errorf("fetching deployments: %w", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deployments:", deployments)

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), deployments); wrote {
		return werr
	}

	if len(deployments) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No container deployments found.")
		return nil
	}

	w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCOMPUTE\tSIZE\tSPOT\tENDPOINT\tCREATED")
	for i := range deployments {
		d := &deployments[i]
		compute := "-"
		size := "-"
		if d.Compute != nil {
			compute = d.Compute.Name
			size = strconv.Itoa(d.Compute.Size)
		}
		spot := "no"
		if d.IsSpot {
			spot = "yes"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name,
			compute,
			size,
			spot,
			d.EndpointBaseURL,
			d.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	return w.Flush()
}
