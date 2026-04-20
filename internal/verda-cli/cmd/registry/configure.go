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
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

const (
	defaultProfileName     = "default"
	defaultExpiresInDays   = 30
	registryConfigureShort = "Configure Verda Container Registry credentials"
)

type configureOptions struct {
	Profile         string
	CredentialsFile string
	Username        string
	PasswordStdin   bool
	Endpoint        string
	ExpiresInDays   int
	Paste           string
	DockerConfig    bool
}

// NewCmdConfigure creates the `verda registry configure` command.
//
// Three input modes: --paste, --username+--password-stdin+--endpoint, or an
// interactive bubbletea wizard when no input flags are supplied on a TTY.
func NewCmdConfigure(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &configureOptions{
		Profile:       defaultProfileName,
		ExpiresInDays: defaultExpiresInDays,
	}

	cmd := &cobra.Command{
		Use:   "configure",
		Short: registryConfigureShort,
		Long: cmdutil.LongDesc(`
			Save Verda Container Registry credentials to the shared credentials
			file. Credentials are stored alongside API and S3 keys using
			verda_registry_ prefixed keys; existing keys in the same profile
			section are preserved.

			Default file: ~/.verda/credentials
			Override with --credentials-file or VERDA_REGISTRY_CREDENTIALS_FILE.

			Two non-interactive input modes are supported:

			  --paste   Paste the full ` + "`docker login ...`" + ` command the Verda
			            web UI prints when you provision a credential.
			  --username + --password-stdin + --endpoint
			            Classic Docker-style: username and endpoint as flags,
			            secret read from stdin.
		`),
		Example: cmdutil.Examples(`
			# Paste the docker login command from the web UI
			verda registry configure --paste "docker login -u vcr-abc+cli -p s3cret vccr.io"

			# Classic form: secret on stdin
			echo -n "$SECRET" | verda registry configure \
			  --username vcr-abc+cli \
			  --endpoint vccr.io \
			  --password-stdin

			# Different profile, custom expiry
			verda registry configure \
			  --profile staging \
			  --paste "docker login -u vcr-u+n -p pw vccr.io" \
			  --expires-in 7
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigure(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared credentials file")
	flags.StringVar(&opts.Username, "username", "", "Registry username (vcr-<project-id>+<name>)")
	flags.BoolVar(&opts.PasswordStdin, "password-stdin", false, "Read the registry secret from stdin")
	flags.StringVar(&opts.Endpoint, "endpoint", "", "Registry host (e.g. \"vccr.io\")")
	flags.IntVar(&opts.ExpiresInDays, "expires-in", opts.ExpiresInDays, "Days from now until the credentials expire")
	flags.StringVar(&opts.Paste, "paste", "", "Full `docker login ...` command to parse (alternative to --username/--password-stdin)")
	flags.BoolVar(&opts.DockerConfig, "docker-config", false, "Also write ~/.docker/config.json (not yet implemented in this subcommand)")

	return cmd
}

// runConfigure is the RunE body, split out for testability.
func runConfigure(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *configureOptions) error {
	// If no non-interactive input mode was supplied and we're not in agent
	// mode, drive the interactive wizard to populate opts.Paste (plus the
	// derived Username/Endpoint) and opts.ExpiresInDays / opts.DockerConfig
	// before falling through to the flag-driven resolution path below.
	if shouldRunConfigureWizard(f, opts) {
		flow := buildConfigureFlow(opts)
		engine := wizard.NewEngine(f.Prompter(), f.Status(),
			wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
		if err := engine.Run(cmd.Context(), flow); err != nil {
			return err
		}
	}

	creds, err := resolveRegistryInputs(cmd, f, ioStreams, opts)
	if err != nil {
		return err
	}

	if opts.DockerConfig {
		_, _ = fmt.Fprintln(ioStreams.ErrOut,
			"TODO: --docker-config is accepted but not yet wired in `registry configure`; "+
				"use `verda registry login` (Task 13) or the interactive wizard (Task 7) "+
				"to also update ~/.docker/config.json.")
	}

	path := credentialsFilePath(opts.CredentialsFile)
	if err := options.WriteRegistryCredentialsToProfile(path, opts.Profile, creds); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Registry credentials saved to profile %q in %s\n", opts.Profile, path)
	if !creds.ExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Credentials expire at %s (in %d days)\n",
			creds.ExpiresAt.Format(time.RFC3339), opts.ExpiresInDays)
	}
	return nil
}

// resolveRegistryInputs decides which non-interactive path to use (paste or
// flags+stdin) and returns populated credentials, or a usage error if neither
// input mode is satisfied.
func resolveRegistryInputs(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *configureOptions) (*options.RegistryCredentials, error) {
	expiresAt := computeExpiry(opts.ExpiresInDays)

	switch {
	case strings.TrimSpace(opts.Paste) != "":
		parsed, err := parseDockerLogin(opts.Paste)
		if err != nil {
			return nil, fmt.Errorf("parse --paste: %w", err)
		}
		return &options.RegistryCredentials{
			Username:  parsed.Username,
			Secret:    parsed.Secret,
			Endpoint:  parsed.Host,
			ProjectID: parsed.ProjectID,
			ExpiresAt: expiresAt,
		}, nil

	case strings.TrimSpace(opts.Username) != "" && opts.PasswordStdin:
		if strings.TrimSpace(opts.Endpoint) == "" {
			return nil, cmdutil.UsageErrorf(cmd, "--endpoint is required with --username/--password-stdin")
		}
		secret, err := readSecretFromStdin(ioStreams.In)
		if err != nil {
			return nil, err
		}
		if secret == "" {
			return nil, cmdutil.UsageErrorf(cmd, "empty secret read from stdin")
		}
		projectID, err := projectIDFromUsername(opts.Username)
		if err != nil {
			return nil, cmdutil.UsageErrorf(cmd, "%s", err.Error())
		}
		return &options.RegistryCredentials{
			Username:  opts.Username,
			Secret:    secret,
			Endpoint:  opts.Endpoint,
			ProjectID: projectID,
			ExpiresAt: expiresAt,
		}, nil

	case f.AgentMode():
		return nil, cmdutil.UsageErrorf(cmd,
			"must provide --paste OR --username + --password-stdin + --endpoint in agent mode")

	default:
		// After the wizard runs, one of the two non-interactive branches
		// above should match. If we land here, the wizard didn't populate
		// opts.Paste (e.g. the user canceled). Fall back to a usage error
		// rather than silently succeeding.
		return nil, cmdutil.UsageErrorf(cmd,
			"must provide --paste OR --username + --password-stdin + --endpoint")
	}
}

// shouldRunConfigureWizard reports whether runConfigure should drive the
// interactive bubbletea wizard. It triggers only when no non-interactive
// input mode was supplied and the caller is not in agent mode.
func shouldRunConfigureWizard(f cmdutil.Factory, opts *configureOptions) bool {
	if f.AgentMode() {
		return false
	}
	if strings.TrimSpace(opts.Paste) != "" {
		return false
	}
	if strings.TrimSpace(opts.Username) != "" && opts.PasswordStdin {
		return false
	}
	return true
}

// computeExpiry returns a clean, second-truncated UTC timestamp for `days`
// from now. A zero or negative `days` yields a zero time (stored as "no
// expiry").
func computeExpiry(days int) time.Time {
	if days <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).UTC().Round(time.Second)
}

// readSecretFromStdin reads the full stdin body and trims a single trailing
// \r\n / \n / \r. Surrounding whitespace is otherwise preserved since the
// secret may legitimately contain leading/trailing spaces.
func readSecretFromStdin(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("stdin is not available")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	s := string(b)
	s = strings.TrimRight(s, "\r\n")
	return s, nil
}

// projectIDFromUsername extracts the <project-id> portion of a registry
// username in the form `vcr-<project-id>+<name>`. Mirrors the regex used by
// parseDockerLogin so both flag and paste paths apply the same rule.
func projectIDFromUsername(username string) (string, error) {
	m := usernameRe.FindStringSubmatch(username)
	if m == nil {
		return "", fmt.Errorf("username must be in format vcr-<project-id>+<name>, got %q", username)
	}
	return m[1], nil
}
