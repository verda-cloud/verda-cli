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

package s3

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

type configureOptions struct {
	Profile         string
	CredentialsFile string
	AccessKey       string
	SecretKey       string
	Endpoint        string
	Region          string
}

// NewCmdConfigure creates the s3 configure command.
func NewCmdConfigure(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &configureOptions{
		Profile: defaultProfileName,
		Region:  defaultRegion,
	}

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure S3 object storage credentials",
		Long: cmdutil.LongDesc(`
			Save S3 object storage credentials to the shared credentials file.

			S3 credentials are stored alongside API credentials using verda_s3_
			prefixed keys. Profile switching (--profile) works across both.

			Default file: ~/.verda/credentials
			Override with --credentials-file or VERDA_SHARED_CREDENTIALS_FILE.
		`),
		Example: cmdutil.Examples(`
			# Interactive wizard (prompts for all fields)
			verda s3 configure

			# Non-interactive with flags
			verda s3 configure \
			  --access-key AKIA1234567890EXAMPLE \
			  --secret-key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLE \
			  --endpoint https://objects.lab.verda.storage

			# Different profile
			verda s3 configure \
			  --profile staging \
			  --access-key AKIA... \
			  --secret-key ... \
			  --endpoint https://staging.objects.verda.storage
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.AccessKey) == "" || strings.TrimSpace(opts.SecretKey) == "" || strings.TrimSpace(opts.Endpoint) == "" {
				flow := buildConfigureFlow(opts)
				engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
				if err := engine.Run(cmd.Context(), flow); err != nil {
					return err
				}
			}

			if strings.TrimSpace(opts.AccessKey) == "" {
				return cmdutil.UsageErrorf(cmd, "--access-key is required")
			}
			if strings.TrimSpace(opts.SecretKey) == "" {
				return cmdutil.UsageErrorf(cmd, "--secret-key is required")
			}
			if strings.TrimSpace(opts.Endpoint) == "" {
				return cmdutil.UsageErrorf(cmd, "--endpoint is required")
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

			section.Key("verda_s3_access_key").SetValue(opts.AccessKey)
			section.Key("verda_s3_secret_key").SetValue(opts.SecretKey)
			section.Key("verda_s3_endpoint").SetValue(opts.Endpoint)
			section.Key("verda_s3_region").SetValue(opts.Region)
			section.Key("verda_s3_auth_mode").SetValue("credentials")

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

			_, _ = fmt.Fprintf(ioStreams.Out, "S3 credentials saved to profile %q in %s\n", opts.Profile, path)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared credentials file")
	flags.StringVar(&opts.AccessKey, "access-key", "", "S3 access key ID")
	flags.StringVar(&opts.SecretKey, "secret-key", "", "S3 secret access key")
	flags.StringVar(&opts.Endpoint, "endpoint", "", "S3 endpoint URL")
	flags.StringVar(&opts.Region, "region", opts.Region, "S3 region")

	return cmd
}
