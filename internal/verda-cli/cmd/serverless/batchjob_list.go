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

func newCmdBatchjobList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List batch-job deployments",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBatchjobList(cmd, f, ioStreams)
		},
	}
	return cmd
}

func runBatchjobList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	jobs, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading batch-job deployments...", func() ([]verda.JobDeploymentShortInfo, error) {
		return client.ServerlessJobs.GetJobDeployments(ctx)
	})
	if err != nil {
		return fmt.Errorf("fetching jobs: %w", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Jobs:", jobs)

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), jobs); wrote {
		return werr
	}

	if len(jobs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No batch-job deployments found.")
		return nil
	}

	w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCOMPUTE\tSIZE\tCREATED")
	for i := range jobs {
		j := &jobs[i]
		compute := "-"
		size := "-"
		if j.Compute != nil {
			compute = j.Compute.Name
			size = strconv.Itoa(j.Compute.Size)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			j.Name,
			compute,
			size,
			j.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	return w.Flush()
}
