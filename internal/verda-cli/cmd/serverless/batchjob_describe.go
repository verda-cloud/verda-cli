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
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdBatchjobDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <name>",
		Aliases: []string{"get", "show"},
		Short:   "Show details of a batch-job deployment",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveBatchjobName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runBatchjobDescribe(cmd, f, ioStreams, name)
		},
	}
	return cmd
}

func resolveBatchjobName(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if f.AgentMode() {
		return "", cmdutil.NewMissingFlagsError([]string{"<name>"})
	}
	return selectBatchjobDeployment(cmd.Context(), f, ioStreams)
}

func selectBatchjobDeployment(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (string, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return "", err
	}

	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	jobs, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading batch-job deployments...", func() ([]verda.JobDeploymentShortInfo, error) {
		return client.ServerlessJobs.GetJobDeployments(listCtx)
	})
	if err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No batch-job deployments found.")
		return "", nil
	}

	labels := make([]string, 0, len(jobs)+1)
	for i := range jobs {
		j := &jobs[i]
		compute := "-"
		if j.Compute != nil {
			compute = fmt.Sprintf("%s x%d", j.Compute.Name, j.Compute.Size)
		}
		labels = append(labels, fmt.Sprintf("%s  (%s)", j.Name, compute))
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select batch-job deployment", labels)
	if err != nil {
		return "", nil // prompter cancel is a clean exit
	}
	if idx == len(jobs) {
		return "", nil
	}
	return jobs[idx].Name, nil
}

func runBatchjobDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	job, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading deployment...", func() (*verda.JobDeployment, error) {
		return client.ServerlessJobs.GetJobDeploymentByName(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("fetching deployment: %w", err)
	}

	var status string
	if s, statusErr := client.ServerlessJobs.GetJobDeploymentStatus(ctx, name); statusErr == nil && s != nil {
		status = s.Status
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deployment:", job)

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), struct {
		*verda.JobDeployment
		Status string `json:"status,omitempty"`
	}{job, status}); wrote {
		return werr
	}

	renderJobDeploymentCard(ioStreams.Out, job, status)
	return nil
}

func renderJobDeploymentCard(w interface{ Write(p []byte) (int, error) }, j *verda.JobDeployment, status string) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	_, _ = fmt.Fprintf(w, "\n  %s\n", label.Render(j.Name))
	if status != "" {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Status"), statusColor(status).Render(status))
	}
	if j.Compute != nil {
		_, _ = fmt.Fprintf(w, "  %s  %s x%d\n", label.Render("Compute"), j.Compute.Name, j.Compute.Size)
	}
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Endpoint"), j.EndpointBaseURL)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Created"), j.CreatedAt.Format("2006-01-02 15:04"))
	if j.Scaling != nil {
		_, _ = fmt.Fprintf(w, "  %s  max=%d  deadline=%ds  ttl=%ds\n",
			label.Render("Scaling"),
			j.Scaling.MaxReplicaCount, j.Scaling.DeadlineSeconds, j.Scaling.QueueMessageTTLSeconds)
	}

	for i := range j.Containers {
		c := &j.Containers[i]
		_, _ = fmt.Fprintf(w, "\n  %s\n", label.Render("Container"))
		_, _ = fmt.Fprintf(w, "    %s  %s\n", label.Render("Image"), c.Image.Image)
		if c.ExposedPort > 0 {
			_, _ = fmt.Fprintf(w, "    %s  %d\n", label.Render("Port"), c.ExposedPort)
		}
		if len(c.Env) > 0 {
			names := make([]string, 0, len(c.Env))
			for _, e := range c.Env {
				names = append(names, e.Name)
			}
			_, _ = fmt.Fprintf(w, "    %s  %s\n", label.Render("Env"), dim.Render(strings.Join(names, ", ")))
		}
	}
	_, _ = fmt.Fprintln(w)
}
