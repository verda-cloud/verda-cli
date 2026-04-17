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
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewCmdShow creates the s3 show command.
func NewCmdShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var profile string
	var credentialsFile string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show S3 credential status",
		Long: cmdutil.LongDesc(`
			Show the current S3 credentials status for a profile.
			Secret values are not displayed — only whether they are loaded.
		`),
		Example: cmdutil.Examples(`
			# Show default profile
			verda s3 show

			# Show specific profile
			verda s3 show --profile staging
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveCredentialsFile(credentialsFile)
			if err != nil {
				return err
			}

			if profile == "" {
				profile = f.Options().AuthOptions.Profile
			}
			if profile == "" {
				profile = "default"
			}

			creds, err := options.LoadS3CredentialsForProfile(path, profile)

			_, _ = fmt.Fprintf(ioStreams.Out, "profile:           %s\n", profile)
			_, _ = fmt.Fprintf(ioStreams.Out, "credentials_file:  %s\n", path)

			if err != nil {
				_, _ = fmt.Fprintf(ioStreams.Out, "s3_configured:     false\n")
				_, _ = fmt.Fprintln(ioStreams.ErrOut)
				_, _ = fmt.Fprintf(ioStreams.ErrOut, "No S3 credentials found for profile %q.\n", profile)
				_, _ = fmt.Fprintf(ioStreams.ErrOut, "Run 'verda s3 configure' to set up S3 credentials.\n")
				return nil //nolint:nilerr // Missing credentials are a valid "not configured" state, not an error.
			}

			_, _ = fmt.Fprintf(ioStreams.Out, "access_key_loaded: %t\n", creds.AccessKey != "")
			_, _ = fmt.Fprintf(ioStreams.Out, "secret_key_loaded: %t\n", creds.SecretKey != "")
			_, _ = fmt.Fprintf(ioStreams.Out, "endpoint:          %s\n", valueOrDash(creds.Endpoint))
			_, _ = fmt.Fprintf(ioStreams.Out, "region:            %s\n", valueOrDash(creds.Region))

			if !creds.HasCredentials() {
				_, _ = fmt.Fprintln(ioStreams.ErrOut)
				var missing []string
				if creds.AccessKey == "" {
					missing = append(missing, "access key")
				}
				if creds.SecretKey == "" {
					missing = append(missing, "secret key")
				}
				if creds.Endpoint == "" {
					missing = append(missing, "endpoint")
				}
				_, _ = fmt.Fprintf(ioStreams.ErrOut, "Missing: %s\n", strings.Join(missing, ", "))
				_, _ = fmt.Fprintf(ioStreams.ErrOut, "Run 'verda s3 configure' to complete setup.\n")
			}

			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&profile, "profile", "", "Credentials profile to show (default: active profile)")
	flags.StringVar(&credentialsFile, "credentials-file", "", "Path to the shared credentials file")

	return cmd
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
