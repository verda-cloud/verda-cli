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
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const envValueMasked = "***"

// ContainerNameOf: the env endpoints key `container_name` on the container
// inside the deployment (the API names it "<deployment>-0"), not on the
// deployment; sending the deployment name returns not_found. Multi-container
// deployments always target Containers[0]; count is the deployment's size.
func ContainerNameOf(ctx context.Context, client *verda.Client, deployment string) (name string, count int, err error) {
	d, err := client.ContainerDeployments.GetDeploymentByName(ctx, deployment)
	if err != nil {
		return "", 0, err
	}
	if len(d.Containers) == 0 {
		return "", 0, fmt.Errorf("deployment %q has no containers", deployment)
	}
	return d.Containers[0].Name, len(d.Containers), nil
}

// EnvVarGroup is one element of the GET environment-variables response,
// which is grouped per container (GetDeploymentEnvVariablesPublicApiResponseDto).
type EnvVarGroup struct {
	ContainerName string                  `json:"container_name"`
	Env           []verda.ContainerEnvVar `json:"env"`
}

// FetchEnvVars reads the grouped response directly. SDK v1.9.0's
// GetEnvironmentVariables decodes it as a flat []ContainerEnvVar, which
// yields one blank entry per container; drop this once the SDK returns groups.
func FetchEnvVars(ctx context.Context, client *verda.Client, deployment string) ([]EnvVarGroup, error) {
	req, err := client.NewRequest(ctx, http.MethodGet, "/container-deployments/"+url.PathEscape(deployment)+"/environment-variables", nil)
	if err != nil {
		return nil, err
	}
	var groups []EnvVarGroup
	if _, err := client.Do(req, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// EnvTarget: the container every env command keys on and its current
// variables. list, set and unset all resolve it the same way so they never
// disagree on which container a multi-container deployment means.
type EnvTarget struct {
	Container  string
	Containers int
	Vars       []verda.ContainerEnvVar
}

// LoadEnvTarget resolves the inner container and reads its variables.
func LoadEnvTarget(ctx context.Context, client *verda.Client, deployment string) (*EnvTarget, error) {
	cn, count, err := ContainerNameOf(ctx, client, deployment)
	if err != nil {
		return nil, err
	}
	groups, err := FetchEnvVars(ctx, client, deployment)
	if err != nil {
		return nil, err
	}
	return &EnvTarget{Container: cn, Containers: count, Vars: EnvVarsOf(groups, cn)}, nil
}

// EnvVarsOf: the variables of container. The deployment GET and the env GET
// are separate reads, so the container may be missing from the groups (no
// vars yet, or a lagging read): that is empty, not an error.
func EnvVarsOf(groups []EnvVarGroup, container string) []verda.ContainerEnvVar {
	for i := range groups {
		if groups[i].ContainerName == container {
			return groups[i].Env
		}
	}
	return nil
}

// PartitionEnvVars splits vars by name: existing ones are updated (PATCH), the
// rest are added (POST). The API rejects a POST for a name that already exists.
func PartitionEnvVars(current, vars []verda.ContainerEnvVar) (toAdd, toUpdate []verda.ContainerEnvVar) {
	existing := make(map[string]bool, len(current))
	for i := range current {
		existing[current[i].Name] = true
	}
	for i := range vars {
		if existing[vars[i].Name] {
			toUpdate = append(toUpdate, vars[i])
		} else {
			toAdd = append(toAdd, vars[i])
		}
	}
	return toAdd, toUpdate
}

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
		Example: cmdutil.Examples(`
			# List variables on a deployment (secret-backed values are masked)
			verda container env list my-endpoint
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

			target, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching environment variables...", func(ctx context.Context) (*EnvTarget, error) {
				return LoadEnvTarget(ctx, client, name)
			})
			if err != nil {
				return err
			}
			if target.Containers > 1 {
				_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: deployment has %d containers; showing %q\n", target.Containers, target.Container)
			}
			vars := target.Vars

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

			newVars, err := BuildEnvVars(envPlain, envSecret)
			if err != nil {
				return err
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			target, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching current variables...", func(ctx context.Context) (*EnvTarget, error) {
				return LoadEnvTarget(ctx, client, name)
			})
			if err != nil {
				return err
			}
			containerName := target.Container
			toAdd, toUpdate := PartitionEnvVars(target.Vars, newVars)

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

			containerName, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading deployment...", func(ctx context.Context) (string, error) {
				cn, _, err := ContainerNameOf(ctx, client, deployment)
				return cn, err
			})
			if err != nil {
				return err
			}

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
