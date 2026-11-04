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
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type containerReplicasResult struct {
	Name           string              `json:"name"`
	Count          int                 `json:"count"`
	CountsByStatus map[string]int      `json:"counts_by_status"`
	Replicas       []verda.ReplicaInfo `json:"replicas"`
}

func newCmdContainerReplicas(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "replicas [<name>]",
		Short: "List the replicas of a container deployment",
		Long: cmdutil.LongDesc(`
			List the current replicas of a container deployment with their status,
			start time, and container image, plus a count per status (running,
			initializing, imagepulling, terminating, error, unavailable). Use it
			after 'verda container scale' to watch replicas start or stop.
		`),
		Example: cmdutil.Examples(`
			verda container replicas my-api
			verda container replicas my-api -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runContainerReplicas(cmd, f, ioStreams, name)
		},
	}
}

func replicaImage(rep *verda.ReplicaInfo) string {
	if rep.Image != "" {
		return rep.Image
	}
	if rep.ImageName == "" {
		return ""
	}
	if rep.ImageTag != "" {
		return rep.ImageName + ":" + rep.ImageTag
	}
	return rep.ImageName
}

func runContainerReplicas(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	replicas, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading replicas...", func(ctx context.Context) (*verda.DeploymentReplicas, error) {
		return client.ContainerDeployments.GetDeploymentReplicas(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("fetching replicas: %w", err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Replicas:", replicas)

	result := containerReplicasResult{Name: name, CountsByStatus: map[string]int{}, Replicas: replicas.List}
	if result.Replicas == nil {
		result.Replicas = []verda.ReplicaInfo{}
	}
	result.Count = len(result.Replicas)
	for i := range result.Replicas {
		result.CountsByStatus[result.Replicas[i].Status]++
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return werr
	}
	return printReplicaTable(ioStreams.Out, &result)
}

func printReplicaTable(w io.Writer, r *containerReplicasResult) error {
	if r.Count == 0 {
		_, _ = fmt.Fprintf(w, "No replicas running for %q.\n", r.Name)
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tSTATUS\tSTARTED\tIMAGE")
	for i := range r.Replicas {
		rep := &r.Replicas[i]
		started := "-"
		if !rep.StartedAt.IsZero() {
			started = rep.StartedAt.Format("2006-01-02 15:04:05")
		}
		style := statusColor(rep.Status)
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", rep.ID, style.Render(rep.Status), started, replicaImage(rep))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	statuses := make([]string, 0, len(r.CountsByStatus))
	for s := range r.CountsByStatus {
		statuses = append(statuses, s)
	}
	slices.Sort(statuses)
	parts := make([]string, 0, len(statuses))
	for _, s := range statuses {
		parts = append(parts, fmt.Sprintf("%d %s", r.CountsByStatus[s], s))
	}
	_, _ = fmt.Fprintf(w, "\n%d replica(s): %s\n", r.Count, strings.Join(parts, ", "))
	return nil
}
