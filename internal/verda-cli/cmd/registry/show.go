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

package registry

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// nearExpiryDays is the threshold at which `show` starts emitting a
// yellow "credentials expire in <N> days" warning line on ErrOut.
const nearExpiryDays = 7

// NewCmdShow creates the `verda registry show` command.
//
// It prints the current Verda Container Registry credential status for a
// profile. The secret value is never displayed. Missing credentials are
// reported as "not configured" and exit 0 — this is a status command, not
// a validator.
func NewCmdShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var profile string
	var credentialsFile string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show Verda Container Registry credential status",
		Long: cmdutil.LongDesc(`
			Show the current Verda Container Registry credential status for a
			profile. The secret is never displayed — only whether credentials
			are loaded, their username, endpoint, project id, and expiry.

			If no credentials are configured for the profile, the command
			reports "not configured" and exits 0.
		`),
		Example: cmdutil.Examples(`
			# Show the default profile
			verda registry show

			# Show a specific profile
			verda registry show --profile staging

			# JSON output (e.g. for scripting)
			verda registry show -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(f, ioStreams, profile, credentialsFile)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&profile, "profile", defaultProfileName, "Credentials profile to show")
	flags.StringVar(&credentialsFile, "credentials-file", "", "Path to the shared credentials file")

	return cmd
}

// runShow is the RunE body, split out for testability.
func runShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams, profile, credentialsFile string) error {
	// Registry commands are in skipCredentialResolution, so AuthOptions.Profile
	// is never resolved. Mirror the s3 convention and fall back to the default
	// profile when --profile was explicitly set to an empty string.
	if profile == "" {
		profile = defaultProfileName
	}

	path := credentialsFilePath(credentialsFile)
	outputFormat := f.OutputFormat()

	creds, err := options.LoadRegistryCredentialsForProfile(path, profile)

	// Treat a missing file, missing section, or empty keys as "not configured".
	// This is intentional — `show` is a status command, not a validator.
	if err != nil || creds == nil || !creds.HasCredentials() {
		payload := map[string]any{
			"registry_configured": false,
			"profile":             profile,
		}
		if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, outputFormat, payload); wrote {
			return werr
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "profile:             %s\n", profile)
		_, _ = fmt.Fprintf(ioStreams.Out, "credentials_file:    %s\n", path)
		_, _ = fmt.Fprintln(ioStreams.Out, "Registry credentials: not configured")
		return nil
	}

	// Configured path.
	expired := creds.IsExpired()
	daysRemaining := creds.DaysRemaining()
	hasExpiry := !creds.ExpiresAt.IsZero()

	payload := map[string]any{
		"registry_configured": true,
		"profile":             profile,
		"username":            creds.Username,
		"endpoint":            creds.Endpoint,
		"project_id":          creds.ProjectID,
	}
	if hasExpiry {
		payload["expires_at"] = creds.ExpiresAt.UTC().Format(time.RFC3339)
		payload["days_remaining"] = daysRemaining
		if expired {
			payload["expired"] = true
		}
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, outputFormat, payload); wrote {
		if werr != nil {
			return werr
		}
		writeExpiryWarnings(ioStreams, creds, expired, hasExpiry, daysRemaining)
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "profile:             %s\n", profile)
	_, _ = fmt.Fprintf(ioStreams.Out, "credentials_file:    %s\n", path)
	_, _ = fmt.Fprintf(ioStreams.Out, "registry_configured: %t\n", true)
	_, _ = fmt.Fprintf(ioStreams.Out, "username:            %s\n", creds.Username)
	_, _ = fmt.Fprintf(ioStreams.Out, "endpoint:            %s\n", creds.Endpoint)
	_, _ = fmt.Fprintf(ioStreams.Out, "project_id:          %s\n", creds.ProjectID)
	if hasExpiry {
		_, _ = fmt.Fprintf(ioStreams.Out, "expires_at:          %s\n", creds.ExpiresAt.UTC().Format(time.RFC3339))
		_, _ = fmt.Fprintf(ioStreams.Out, "days_remaining:      %d\n", daysRemaining)
		if expired {
			_, _ = fmt.Fprintf(ioStreams.Out, "expired:             %t\n", true)
		}
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "expires_at:          %s\n", "(none)")
	}

	writeExpiryWarnings(ioStreams, creds, expired, hasExpiry, daysRemaining)
	return nil
}

// writeExpiryWarnings prints a red ErrOut line when credentials are past
// their expiry, and a yellow ErrOut line when they are within
// nearExpiryDays of expiring. No warning is emitted when expiry is unknown
// (zero time) or more than nearExpiryDays out.
func writeExpiryWarnings(ioStreams cmdutil.IOStreams, creds *options.RegistryCredentials, expired, hasExpiry bool, daysRemaining int) {
	if !hasExpiry {
		return
	}
	// Match existing convention: red = Color("1"), yellow = Color("3").
	// See e.g. cmd/status/status.go, cmd/vm/action.go, cmd/s3/rm.go.
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	expiryDate := creds.ExpiresAt.UTC().Format("2006-01-02")
	switch {
	case expired:
		daysAgo := -daysRemaining
		_, _ = fmt.Fprintln(ioStreams.ErrOut, redStyle.Render(fmt.Sprintf(
			"WARNING: registry credentials expired on %s (%d days ago)",
			expiryDate, daysAgo,
		)))
	case daysRemaining < nearExpiryDays:
		_, _ = fmt.Fprintln(ioStreams.ErrOut, yellowStyle.Render(fmt.Sprintf(
			"Registry credentials expire in %d days (on %s)",
			daysRemaining, expiryDate,
		)))
	}
}
