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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const envValueMasked = "***"

func newCmdContainerEnv(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variables on a running deployment",
		Long: cmdutil.LongDesc(`
			List, set, and unset environment variables on a running container
			deployment without a full redeployment.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		newCmdEnvList(f, ioStreams),
		newCmdEnvSet(f, ioStreams),
		newCmdEnvUnset(f, ioStreams),
	)
	return cmd
}

// --- list ---

func newCmdEnvList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "list <deployment>",
		Aliases: []string{"ls"},
		Short:   "List environment variables",
		Args:    cobra.MaximumNArgs(1),
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

			vars, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching environment variables...", func(ctx context.Context) ([]verda.ContainerEnvVar, error) {
				return client.ContainerDeployments.GetEnvironmentVariables(ctx, name)
			})
			if err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), vars); wrote {
				return werr
			}
			if len(vars) == 0 {
				_, _ = fmt.Fprintf(ioStreams.Out, "No environment variables set on deployment %q.\n", name)
				return nil
			}

			w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tTYPE\tVALUE")
			for _, v := range vars {
				val := v.ValueOrReferenceToSecret
				if v.Type == envTypeSecret {
					val = envValueMasked
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", v.Name, v.Type, val)
			}
			return w.Flush()
		},
	}
}

// --- set ---

func newCmdEnvSet(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var envPlain, envSecret []string

	cmd := &cobra.Command{
		Use:   "set <deployment>",
		Short: "Set environment variables on a deployment",
		Long: cmdutil.LongDesc(`
			Add or update environment variables on a running deployment. New
			variables are added; existing variables are updated in place.

			Use --env for plain-text variables and --env-secret for variables
			backed by a secret reference.
		`),
		Example: cmdutil.Examples(`
			# Set a plain variable
			verda container env set my-endpoint --env HF_HOME=/data/.huggingface

			# Set a secret-backed variable
			verda container env set my-endpoint --env-secret API_KEY=my-secret

			# Set multiple at once
			verda container env set my-endpoint \
			  --env HF_HOME=/data/.huggingface \
			  --env MODEL_NAME=llama-3 \
			  --env-secret API_KEY=my-secret
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if f.AgentMode() && len(envPlain) == 0 && len(envSecret) == 0 {
				return cmdutil.NewMissingFlagsError([]string{"--env or --env-secret"})
			}
			if len(envPlain) == 0 && len(envSecret) == 0 {
				return cmdutil.UsageErrorf(cmd, "at least one --env or --env-secret is required")
			}

			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil {
				return err
			}
			if name == "" {
				return nil
			}

			newVars, err := buildEnvVars(envPlain, envSecret)
			if err != nil {
				return err
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			current, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching current variables...", func(ctx context.Context) ([]verda.ContainerEnvVar, error) {
				return client.ContainerDeployments.GetEnvironmentVariables(ctx, name)
			})
			if err != nil {
				return err
			}

			existing := make(map[string]bool, len(current))
			for _, v := range current {
				existing[v.Name] = true
			}

			var toAdd, toUpdate []verda.ContainerEnvVar
			for i := range newVars {
				if existing[newVars[i].Name] {
					toUpdate = append(toUpdate, newVars[i])
				} else {
					toAdd = append(toAdd, newVars[i])
				}
			}

			containerName := resolveContainerNameFromDeployment(name)

			if len(toAdd) > 0 {
				req := &verda.ContainerEnvVarsRequest{ContainerName: containerName, Env: toAdd}
				cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Add env vars:", req)
				if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Adding environment variables...", func(ctx context.Context) error {
					return client.ContainerDeployments.AddEnvironmentVariables(ctx, name, req)
				}); err != nil {
					return err
				}
			}
			if len(toUpdate) > 0 {
				req := &verda.ContainerEnvVarsRequest{ContainerName: containerName, Env: toUpdate}
				cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Update env vars:", req)
				if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Updating environment variables...", func(ctx context.Context) error {
					return client.ContainerDeployments.UpdateEnvironmentVariables(ctx, name, req)
				}); err != nil {
					return err
				}
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]any{
				"deployment": name, "added": len(toAdd), "updated": len(toUpdate),
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Updated environment variables for deployment %q (%d added, %d updated)\n",
				name, len(toAdd), len(toUpdate))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&envPlain, flagEnv, nil, "Plain environment variable KEY=VALUE; repeat for multiple")
	cmd.Flags().StringArrayVar(&envSecret, flagEnvSecret, nil, "Secret-backed env KEY=SECRET_NAME; repeat for multiple")
	return cmd
}

// resolveContainerNameFromDeployment returns the container name for env var
// operations. Serverless deployments have a single container whose name
// matches the deployment name.
func resolveContainerNameFromDeployment(deploymentName string) string {
	return deploymentName
}

// --- unset ---

func newCmdEnvUnset(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var deployment string
	var yes bool

	cmd := &cobra.Command{
		Use:   "unset KEY [KEY...]",
		Short: "Remove environment variables from a deployment",
		Example: cmdutil.Examples(`
			# Remove one variable
			verda container env unset --deployment my-endpoint HF_HOME

			# Remove multiple
			verda container env unset --deployment my-endpoint HF_HOME MODEL_NAME
		`),
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if deployment == "" {
				if f.AgentMode() {
					return cmdutil.NewMissingFlagsError([]string{"--deployment"})
				}
				name, err := resolveContainerName(cmd, f, ioStreams, nil)
				if err != nil {
					return err
				}
				if name == "" {
					return nil
				}
				deployment = name
			}

			if f.AgentMode() && !yes {
				return cmdutil.NewConfirmationRequiredError("unset environment variables")
			}
			if !yes {
				ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
					fmt.Sprintf("Remove %d variable(s) from %s", len(args), deployment),
					"Variables: "+strings.Join(args, ", "),
					"Remove these environment variables?")
				if err != nil {
					return err
				}
				if !ok {
					return nil
				}
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			containerName := resolveContainerNameFromDeployment(deployment)
			req := &verda.DeleteContainerEnvVarsRequest{
				ContainerName: containerName,
				Env:           args,
			}
			cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete env vars:", req)

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Removing environment variables...", func(ctx context.Context) error {
				return client.ContainerDeployments.DeleteEnvironmentVariables(ctx, deployment, req)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]any{
				"deployment": deployment, "removed": len(args),
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Removed %d environment variable(s) from deployment %q\n", len(args), deployment)
			return nil
		},
	}
	cmd.Flags().StringVar(&deployment, "deployment", "", "Deployment name")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}
