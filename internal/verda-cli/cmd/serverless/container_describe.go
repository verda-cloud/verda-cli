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

func newCmdContainerDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <name>",
		Aliases: []string{"get", "show"},
		Short:   "Show details of a container deployment",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runContainerDescribe(cmd, f, ioStreams, name)
		},
	}
	return cmd
}

// resolveContainerName returns the first positional arg when present; otherwise
// prompts interactively. Agent mode returns a MISSING_REQUIRED_FLAGS error when
// no name is supplied because prompts are blocked.
func resolveContainerName(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if f.AgentMode() {
		return "", cmdutil.NewMissingFlagsError([]string{"<name>"})
	}
	return selectContainerDeployment(cmd.Context(), f, ioStreams)
}

// selectContainerDeployment loads all container deployments and runs a single-select
// picker. Returns "" (no error) when the user cancels or there are no deployments.
func selectContainerDeployment(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (string, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return "", err
	}

	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	deployments, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading container deployments...", func() ([]verda.ContainerDeployment, error) {
		return client.ContainerDeployments.GetDeployments(listCtx)
	})
	if err != nil {
		return "", err
	}
	if len(deployments) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No container deployments found.")
		return "", nil
	}

	labels := make([]string, 0, len(deployments)+1)
	for i := range deployments {
		d := &deployments[i]
		compute := "-"
		if d.Compute != nil {
			compute = fmt.Sprintf("%s x%d", d.Compute.Name, d.Compute.Size)
		}
		labels = append(labels, fmt.Sprintf("%s  (%s)", d.Name, compute))
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select container deployment", labels)
	if err != nil {
		return "", nil // prompter cancel is a clean exit
	}
	if idx == len(deployments) {
		return "", nil
	}
	return deployments[idx].Name, nil
}

func runContainerDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	deployment, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading deployment...", func() (*verda.ContainerDeployment, error) {
		return client.ContainerDeployments.GetDeploymentByName(ctx, name)
	})
	if err != nil {
		return fmt.Errorf("fetching deployment: %w", err)
	}

	// Best-effort status fetch — don't fail the describe if status errors.
	var status string
	s, statusErr := client.ContainerDeployments.GetDeploymentStatus(ctx, name)
	if statusErr == nil && s != nil {
		status = s.Status
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deployment:", deployment)

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), struct {
		*verda.ContainerDeployment
		Status string `json:"status,omitempty"`
	}{deployment, status}); wrote {
		return werr
	}

	renderContainerDeploymentCard(ioStreams.Out, deployment, status)
	return nil
}

func renderContainerDeploymentCard(w interface{ Write(p []byte) (int, error) }, d *verda.ContainerDeployment, status string) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	_, _ = fmt.Fprintf(w, "\n  %s\n", label.Render(d.Name))
	if status != "" {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Status"), statusColor(status).Render(status))
	}
	if d.Compute != nil {
		_, _ = fmt.Fprintf(w, "  %s  %s x%d\n", label.Render("Compute"), d.Compute.Name, d.Compute.Size)
	}
	spotLabel := "on-demand"
	if d.IsSpot {
		spotLabel = "spot"
	}
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Billing"), spotLabel)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Endpoint"), d.EndpointBaseURL)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Created"), d.CreatedAt.Format("2006-01-02 15:04"))

	if d.ContainerRegistrySettings != nil {
		reg := "public"
		if d.ContainerRegistrySettings.IsPrivate && d.ContainerRegistrySettings.Credentials != nil {
			reg = d.ContainerRegistrySettings.Credentials.Name
		}
		_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Registry"), reg)
	}

	for i := range d.Containers {
		c := &d.Containers[i]
		_, _ = fmt.Fprintf(w, "\n  %s\n", label.Render("Container"))
		_, _ = fmt.Fprintf(w, "    %s  %s\n", label.Render("Image"), c.Image.Image)
		if c.ExposedPort > 0 {
			_, _ = fmt.Fprintf(w, "    %s  %d\n", label.Render("Port"), c.ExposedPort)
		}
		if c.Healthcheck != nil && c.Healthcheck.Enabled {
			_, _ = fmt.Fprintf(w, "    %s  %s on port %d\n", label.Render("Healthcheck"), c.Healthcheck.Path, c.Healthcheck.Port)
		}
		if len(c.Env) > 0 {
			names := make([]string, 0, len(c.Env))
			for _, e := range c.Env {
				names = append(names, e.Name)
			}
			_, _ = fmt.Fprintf(w, "    %s  %s\n", label.Render("Env"), dim.Render(strings.Join(names, ", ")))
		}
		if len(c.VolumeMounts) > 0 {
			mounts := make([]string, 0, len(c.VolumeMounts))
			for _, m := range c.VolumeMounts {
				mounts = append(mounts, fmt.Sprintf("%s:%s", m.Type, m.MountPath))
			}
			_, _ = fmt.Fprintf(w, "    %s  %s\n", label.Render("Mounts"), dim.Render(strings.Join(mounts, ", ")))
		}
	}
	_, _ = fmt.Fprintln(w)
}
