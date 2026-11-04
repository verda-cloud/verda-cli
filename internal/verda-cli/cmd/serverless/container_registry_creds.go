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
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	regTypeVerda     = "verda"
	regTypeGCR       = "gcr"
	regTypeDockerhub = "dockerhub"
	regTypeGHCR      = "ghcr"
	regTypeAWSECR    = "aws-ecr"
	regTypeScaleway  = "scaleway"
	regTypeCustom    = "custom"
)

var registryTypes = []string{
	regTypeVerda, regTypeGCR, regTypeDockerhub, regTypeGHCR,
	regTypeAWSECR, regTypeScaleway, regTypeCustom,
}

func newCmdContainerRegistryCreds(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry-creds",
		Short: "Manage container registry credentials",
		Long: cmdutil.LongDesc(`
			Create, list, and delete registry credentials for pulling private
			container images in serverless deployments.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		newCmdRegistryCredsList(f, ioStreams),
		newCmdRegistryCredsCreate(f, ioStreams),
		newCmdRegistryCredsDelete(f, ioStreams),
	)
	return cmd
}

// --- list ---

func newCmdRegistryCredsList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registry credentials",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := f.VerdaClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
			defer cancel()

			creds, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching registry credentials...", func(ctx context.Context) ([]verda.RegistryCredentials, error) {
				return client.ContainerDeployments.GetRegistryCredentials(ctx)
			})
			if err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), creds); wrote {
				return werr
			}
			if len(creds) == 0 {
				_, _ = fmt.Fprintln(ioStreams.Out, "No registry credentials found.")
				return nil
			}
			w := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tCREATED")
			for _, c := range creds {
				_, _ = fmt.Fprintf(w, "%s\t%s\n", c.Name, c.CreatedAt.Format("2006-01-02 15:04"))
			}
			return w.Flush()
		},
	}
}

// --- create ---

type registryCredsOptions struct {
	Name              string
	Type              string
	Username          string
	AccessToken       string
	ServiceAccountKey string
	DockerConfigJSON  string
	AccessKeyID       string
	SecretAccessKey   string
	Region            string
	ECRRepo           string
}

func newCmdRegistryCredsCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &registryCredsOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create registry credentials",
		Long: cmdutil.LongDesc(`
			Create credentials for pulling images from a private container
			registry. The required fields depend on the registry type:

			  verda, dockerhub, ghcr: --username + --access-token
			  gcr: --service-account-key
			  aws-ecr: --access-key-id + --secret-access-key + --region + --ecr-repo
			  scaleway: --username + --access-token
			  custom: --docker-config-json
		`),
		Example: cmdutil.Examples(`
			# Docker Hub
			verda container registry-creds create \
			  --name dockerhub-creds --type dockerhub \
			  --username myuser --access-token dckr_pat_xxx

			# GCR with service account key file
			verda container registry-creds create \
			  --name gcr-creds --type gcr \
			  --service-account-key "$(cat sa-key.json)"
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRegistryCredsCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Credentials name")
	flags.StringVar(&opts.Type, "type", "", "Registry type: "+strings.Join(registryTypes, " | "))
	flags.StringVar(&opts.Username, "username", "", "Username (verda, dockerhub, ghcr, scaleway)")
	flags.StringVar(&opts.AccessToken, "access-token", "", "Access token or password")
	flags.StringVar(&opts.ServiceAccountKey, "service-account-key", "", "GCR service account key (JSON)")
	flags.StringVar(&opts.DockerConfigJSON, "docker-config-json", "", "Docker config.json contents (custom type)")
	flags.StringVar(&opts.AccessKeyID, "access-key-id", "", "AWS access key ID (aws-ecr)")
	flags.StringVar(&opts.SecretAccessKey, "secret-access-key", "", "AWS secret access key (aws-ecr)")
	flags.StringVar(&opts.Region, "region", "", "AWS region (aws-ecr)")
	flags.StringVar(&opts.ECRRepo, "ecr-repo", "", "ECR repository URL (aws-ecr)")
	return cmd
}

func runRegistryCredsCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *registryCredsOptions) error {
	if f.AgentMode() {
		var missing []string
		if opts.Name == "" {
			missing = append(missing, "--name")
		}
		if opts.Type == "" {
			missing = append(missing, "--type")
		}
		if len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	}

	if opts.Name == "" {
		v, err := f.Prompter().TextInput(cmd.Context(), "Credentials name:")
		if err != nil {
			if isPromptCancel(err) {
				return nil
			}
			return err
		}
		opts.Name = v
	}

	if opts.Type == "" {
		idx, err := f.Prompter().Select(cmd.Context(), "Registry type:", registryTypes, tui.WithShowHints(true))
		if err != nil {
			if isPromptCancel(err) {
				return nil
			}
			return err
		}
		opts.Type = registryTypes[idx]
	}

	if err := promptRegistryTypeFields(cmd.Context(), f.Prompter(), opts); err != nil {
		if isPromptCancel(err) {
			return nil
		}
		return err
	}

	if err := validateRegistryCreds(opts); err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	req := &verda.CreateRegistryCredentialsRequest{
		Name:              opts.Name,
		Type:              opts.Type,
		Username:          opts.Username,
		AccessToken:       opts.AccessToken,
		ServiceAccountKey: opts.ServiceAccountKey,
		DockerConfigJson:  opts.DockerConfigJSON,
		AccessKeyID:       opts.AccessKeyID,
		SecretAccessKey:   opts.SecretAccessKey,
		Region:            opts.Region,
		EcrRepo:           opts.ECRRepo,
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Creating registry credentials...", func(ctx context.Context) error {
		return client.ContainerDeployments.CreateRegistryCredentials(ctx, req)
	}); err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
		"name": opts.Name, "type": opts.Type, "action": "created",
	}); wrote {
		return werr
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Created registry credentials %q (type: %s)\n", opts.Name, opts.Type)
	return nil
}

// promptRegistryTypeFields prompts for missing type-specific fields interactively.
func promptRegistryTypeFields(ctx context.Context, p tui.Prompter, opts *registryCredsOptions) error {
	switch opts.Type {
	case regTypeVerda, regTypeDockerhub, regTypeGHCR, regTypeScaleway:
		return promptTokenCreds(ctx, p, opts)
	case regTypeGCR:
		return promptGCRCreds(ctx, p, opts)
	case regTypeAWSECR:
		return promptAWSECRCreds(ctx, p, opts)
	case regTypeCustom:
		return promptCustomCreds(ctx, p, opts)
	}
	return nil
}

func promptTokenCreds(ctx context.Context, p tui.Prompter, opts *registryCredsOptions) error {
	if opts.Username == "" {
		v, err := p.TextInput(ctx, "Username:")
		if err != nil {
			return err
		}
		opts.Username = v
	}
	if opts.AccessToken == "" {
		v, err := p.Password(ctx, "Access token:")
		if err != nil {
			return err
		}
		opts.AccessToken = v
	}
	return nil
}

func promptGCRCreds(ctx context.Context, p tui.Prompter, opts *registryCredsOptions) error {
	if opts.ServiceAccountKey == "" {
		v, err := p.TextInput(ctx, "Service account key (JSON):")
		if err != nil {
			return err
		}
		opts.ServiceAccountKey = v
	}
	return nil
}

func promptAWSECRCreds(ctx context.Context, p tui.Prompter, opts *registryCredsOptions) error {
	if opts.AccessKeyID == "" {
		v, err := p.TextInput(ctx, "AWS access key ID:")
		if err != nil {
			return err
		}
		opts.AccessKeyID = v
	}
	if opts.SecretAccessKey == "" {
		v, err := p.Password(ctx, "AWS secret access key:")
		if err != nil {
			return err
		}
		opts.SecretAccessKey = v
	}
	if opts.Region == "" {
		v, err := p.TextInput(ctx, "AWS region:")
		if err != nil {
			return err
		}
		opts.Region = v
	}
	if opts.ECRRepo == "" {
		v, err := p.TextInput(ctx, "ECR repository URL:")
		if err != nil {
			return err
		}
		opts.ECRRepo = v
	}
	return nil
}

func promptCustomCreds(ctx context.Context, p tui.Prompter, opts *registryCredsOptions) error {
	if opts.DockerConfigJSON == "" {
		v, err := p.TextInput(ctx, "Docker config.json contents:")
		if err != nil {
			return err
		}
		opts.DockerConfigJSON = v
	}
	return nil
}

func validateRegistryCreds(opts *registryCredsOptions) error {
	validType := slices.Contains(registryTypes, opts.Type)
	if !validType {
		return fmt.Errorf("invalid --type %q: expected one of %s", opts.Type, strings.Join(registryTypes, ", "))
	}

	switch opts.Type {
	case regTypeVerda, regTypeDockerhub, regTypeGHCR, regTypeScaleway:
		if opts.Username == "" || opts.AccessToken == "" {
			return fmt.Errorf("type %q requires --username and --access-token", opts.Type)
		}
	case regTypeGCR:
		if opts.ServiceAccountKey == "" {
			return fmt.Errorf("type %q requires --service-account-key", opts.Type)
		}
	case regTypeAWSECR:
		if opts.AccessKeyID == "" || opts.SecretAccessKey == "" || opts.Region == "" || opts.ECRRepo == "" {
			return fmt.Errorf("type %q requires --access-key-id, --secret-access-key, --region, and --ecr-repo", opts.Type)
		}
	case regTypeCustom:
		if opts.DockerConfigJSON == "" {
			return fmt.Errorf("type %q requires --docker-config-json", opts.Type)
		}
	}
	return nil
}

// --- delete ---

func newCmdRegistryCredsDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var force, yes bool

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "del"},
		Short:   "Delete registry credentials",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if f.AgentMode() && !yes {
				return cmdutil.NewConfirmationRequiredError("delete registry credentials")
			}
			if !yes {
				detail := ""
				if force {
					detail = "Force-deleting will remove the credentials even if they are in use."
				}
				ok, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
					"Delete registry credentials "+name, detail, "Delete credentials "+name+"?")
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

			if err := cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting registry credentials...", func(ctx context.Context) error {
				return client.ContainerDeployments.DeleteRegistryCredentials(ctx, name, force)
			}); err != nil {
				return err
			}

			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
				"name": name, "action": "deleted",
			}); wrote {
				return werr
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "Deleted registry credentials %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Force delete even if credentials are in use")
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, "Skip confirmation (required in agent mode)")
	return cmd
}
