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

package auth

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/ini.v1"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

type loginOptions struct {
	Profile         string
	CredentialsFile string
	BaseURL         string
	ClientID        string
	ClientSecret    string
}

// NewCmdLogin creates the auth login command.
func NewCmdLogin(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &loginOptions{
		Profile: "default",
		BaseURL: "https://api.verda.com/v1",
	}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save API credentials for a profile",
		Long: cmdutil.LongDesc(`
			Save credentials to a shared credentials file in AWS-style INI format.

			Default file: ~/.verda/credentials
			Override with --credentials-file or VERDA_SHARED_CREDENTIALS_FILE.

			Credentials are stored per profile:
			  [default]
			  verda_base_url      = https://api.verda.com/v1
			  verda_client_id     = your-client-id
			  verda_client_secret = your-client-secret
		`),
		Example: cmdutil.Examples(`
			# Interactive wizard (prompts for all fields)
			verda auth login

			# Non-interactive with flags
			verda auth login \
			  --client-id your-client-id \
			  --client-secret your-client-secret

			# Staging profile with custom API endpoint
			verda auth login \
			  --profile staging \
			  --base-url https://staging-api.verda.com/v1 \
			  --client-id staging-id \
			  --client-secret staging-secret

			# Custom credentials file
			verda auth login \
			  --credentials-file /path/to/credentials \
			  --client-id your-id \
			  --client-secret your-secret
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.ClientID) == "" || strings.TrimSpace(opts.ClientSecret) == "" {
				flow := buildLoginFlow(opts)
				engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
				if err := engine.Run(cmd.Context(), flow); err != nil {
					return err
				}
			}

			if strings.TrimSpace(opts.ClientID) == "" {
				return cmdutil.UsageErrorf(cmd, "--client-id is required")
			}
			if strings.TrimSpace(opts.ClientSecret) == "" {
				return cmdutil.UsageErrorf(cmd, "--client-secret is required")
			}

			path, err := resolveCredentialsFile(opts.CredentialsFile)
			if err != nil {
				return err
			}

			cfg := ini.Empty()
			if _, err := os.Stat(path); err == nil {
				cfg, err = ini.Load(path)
				if err != nil {
					return err
				}
			}

			section, err := cfg.GetSection(opts.Profile)
			if err != nil {
				section, err = cfg.NewSection(opts.Profile)
				if err != nil {
					return err
				}
			}

			section.Key("verda_base_url").SetValue(opts.BaseURL)
			section.Key("verda_client_id").SetValue(opts.ClientID)
			section.Key("verda_client_secret").SetValue(opts.ClientSecret)

			if _, err := options.EnsureVerdaDir(); err != nil {
				return err
			}
			if err := cfg.SaveTo(path); err != nil {
				return err
			}
			// Restrict file permissions on Unix (no-op on Windows).
			if runtime.GOOS != "windows" {
				_ = os.Chmod(path, 0o600)
			}

			_, _ = fmt.Fprintf(ioStreams.Out, "Saved profile %q to %s\n", opts.Profile, path)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared credentials file")
	flags.StringVar(&opts.BaseURL, "base-url", opts.BaseURL, "Verda API base URL")
	flags.StringVar(&opts.ClientID, "client-id", "", "Verda API client ID")
	flags.StringVar(&opts.ClientSecret, "client-secret", "", "Verda API client secret")

	return cmd
}
