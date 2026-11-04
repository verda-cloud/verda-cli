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
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
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

// resolveContainerName: args[0], else picker; agent, pipes and -o json|yaml require <name>.
func resolveContainerName(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if !interactiveTTY(f) {
		return "", cmdutil.NewMissingFlagsError([]string{"<name>"})
	}
	return selectContainerDeployment(cmd.Context(), f, ioStreams)
}

// selectContainerDeployment prompts for a deployment; cancel/empty list → "", nil.
func selectContainerDeployment(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (string, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return "", err
	}

	listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	deployments, err := cmdutil.WithSpinner(listCtx, f.Status(), "Loading container deployments...", func(ctx context.Context) ([]verda.ContainerDeployment, error) {
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

	idx, err := f.Prompter().Select(ctx, "Select container deployment", labels, tui.WithShowHints(true))
	if err != nil {
		if isPromptCancel(err) {
			return "", nil
		}
		return "", err
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

	// Status and scaling fetched under the same spinner; best-effort with a
	// short subordinate timeout so a slow RPC can't blank it. Describe still
	// succeeds if either fails.
	var status string
	var scaling *verda.ContainerScalingOptions
	var scalingErr error
	deployment, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading deployment...", func(ctx context.Context) (*verda.ContainerDeployment, error) {
		d, derr := client.ContainerDeployments.GetDeploymentByName(ctx, name)
		if derr != nil {
			return nil, derr
		}
		statusCtx, statusCancel := context.WithTimeout(ctx, statusRPCTimeout)
		defer statusCancel()
		if s, statusErr := client.ContainerDeployments.GetDeploymentStatus(statusCtx, name); statusErr == nil && s != nil {
			status = s.Status
		}
		scalingCtx, scalingCancel := context.WithTimeout(ctx, statusRPCTimeout)
		defer scalingCancel()
		scaling, scalingErr = client.ContainerDeployments.GetDeploymentScaling(scalingCtx, name)
		return d, nil
	})
	if err != nil {
		return fmt.Errorf("fetching deployment: %w", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deployment:", deployment)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Scaling:", scaling)

	var scalingErrMsg string
	if scalingErr != nil {
		scalingErrMsg = scalingErr.Error()
	}
	// Embed adds fields alongside SDK ones; if SDK gains json:"status", switch to explicit fields.
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), struct {
		*verda.ContainerDeployment
		Status       string                         `json:"status,omitempty"`
		Scaling      *verda.ContainerScalingOptions `json:"scaling,omitempty"`
		ScalingError string                         `json:"scaling_error,omitempty"`
	}{deployment, status, scaling, scalingErrMsg}); wrote {
		return werr
	}

	renderContainerDeploymentCard(ioStreams.Out, deployment, status)
	renderDescribeScaling(ioStreams.Out, scaling, scalingErr)
	return nil
}

// renderDescribeScaling: a failed scaling read leaves a note, not an error.
func renderDescribeScaling(w io.Writer, sc *verda.ContainerScalingOptions, err error) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if err != nil || sc == nil {
		msg := "unavailable"
		if err != nil {
			msg += ": " + err.Error()
		}
		_, _ = fmt.Fprintf(w, "  %s  %s\n\n", label.Render("Scaling"), dim.Render(msg))
		return
	}
	_, _ = fmt.Fprintf(w, "  %s\n", label.Render("Scaling"))
	writeScalingLines(w, "    ", sc)
	_, _ = fmt.Fprintln(w)
}

func renderContainerDeploymentCard(w io.Writer, d *verda.ContainerDeployment, status string) {
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
