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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdContainerReplicas(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "replicas <deployment>",
		Short: "List replicas for a container deployment",
		Long: cmdutil.LongDesc(`
			Show the individual replicas of a container deployment with their
			status, start time, and container image.
		`),
		Example: cmdutil.Examples(`
			# List replicas by name
			verda container replicas my-endpoint

			# JSON output
			verda container replicas my-endpoint -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil {
				return err
			}
			if name == "" {
				return nil
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			replicas, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching replicas...", func(ctx context.Context) (*verda.DeploymentReplicas, error) {
				return client.ContainerDeployments.GetDeploymentReplicas(ctx, name)
			})
			if err != nil {
				return err
			}

			cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Replicas:", replicas)

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), replicas); wrote {
				return werr
			}
			if len(replicas.List) == 0 {
				_, _ = fmt.Fprintf(ioStreams.Out, "No replicas found for deployment %q.\n", name)
				return nil
			}

			w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tSTATUS\tSTARTED\tIMAGE")
			for i := range replicas.List {
				r := &replicas.List[i]
				image := r.Image
				if image == "" && r.ImageName != "" {
					image = r.ImageName
					if r.ImageTag != "" {
						image += ":" + r.ImageTag
					}
				}
				style := statusColor(r.Status)
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					r.ID,
					style.Render(r.Status),
					r.StartedAt.Format("2006-01-02 15:04"),
					image)
			}
			return w.Flush()
		},
	}
}
