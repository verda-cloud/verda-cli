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

func newCmdContainerSecret(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage serverless secrets",
		Long: cmdutil.LongDesc(`
			Create, list, and delete secrets used by serverless deployments.
			Secrets are project-level resources shared across all deployments.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		newCmdSecretList(f, ioStreams),
		newCmdSecretCreate(f, ioStreams),
		newCmdSecretDelete(f, ioStreams),
	)
	return cmd
}

// --- list ---

func newCmdSecretList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List secrets",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			secrets, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching secrets...", func(ctx context.Context) ([]verda.Secret, error) {
				return client.ContainerDeployments.GetSecrets(ctx)
			})
			if err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), secrets); wrote {
				return werr
			}
			if len(secrets) == 0 {
				_, _ = fmt.Fprintln(ioStreams.Out, "No secrets found.")
				return nil
			}
			w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tTYPE\tCREATED")
			for _, s := range secrets {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.SecretType, s.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

// --- create ---

func newCmdSecretCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var name, value string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a secret",
		Example: cmdutil.Examples(`
			# Create a secret with flags
			verda container secret create --name my-secret --value s3cr3t

			# Interactive (prompts for name and value)
			verda container secret create
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.AgentMode() {
				var missing []string
				if name == "" {
					missing = append(missing, "--name")
				}
				if value == "" {
					missing = append(missing, "--value")
				}
				if len(missing) > 0 {
					return cmdutil.NewMissingFlagsError(missing)
				}
			}

			if name == "" {
				v, err := f.Prompter().TextInput(cmd.Context(), "Secret name:")
				if err != nil {
					if isPromptCancel(err) {
						return nil
					}
					return err
				}
				name = v
			}
			if value == "" {
				v, err := f.Prompter().Password(cmd.Context(), "Secret value:")
				if err != nil {
					if isPromptCancel(err) {
						return nil
					}
					return err
				}
				value = v
			}

			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			req := &verda.CreateSecretRequest{Name: name, Value: value}
			cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Creating secret...", func(ctx context.Context) error {
				return client.ContainerDeployments.CreateSecret(ctx, req)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
				"name": name, "action": "created",
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Created secret %q\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Secret name")
	cmd.Flags().StringVar(&value, "value", "", "Secret value")
	return cmd
}

// --- delete ---

func newCmdSecretDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var force, yes bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "del"},
		Short:   "Delete a secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if f.AgentMode() && !yes {
				return cmdutil.NewConfirmationRequiredError("delete secret")
			}
			if !yes {
				detail := ""
				if force {
					detail = "Force-deleting will remove the secret even if it is in use."
				}
				ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
					"Delete secret "+name, detail, "Delete secret "+name+"?")
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

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting secret...", func(ctx context.Context) error {
				return client.ContainerDeployments.DeleteSecret(ctx, name, force)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
				"name": name, "action": "deleted",
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Deleted secret %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force delete even if the secret is in use")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}
