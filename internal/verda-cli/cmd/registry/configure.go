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
	"context"
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

	// defaultRegistryEndpoint is the production VCR host used when the user
	// runs `--username/--password-stdin` without an explicit `--endpoint`
	// AND no endpoint has been saved for the profile. Staging and custom
	// deployments require either `--paste` (which carries the host inline)
	// or an explicit `--endpoint` flag.
	defaultRegistryEndpoint = "vccr.io"
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

	// Secret is populated only by the wizard's manual-entry path (the
	// "Enter credential name + secret" mode). The flag path reads the secret
	// from stdin instead, so it stays empty there. resolveRegistryInputs uses
	// a non-empty Secret to distinguish the wizard-manual path from the
	// --password-stdin path.
	Secret string
}

// NewCmdConfigure creates the `verda registry configure` command.
//
// Three input modes: --paste, --username+--password-stdin (with --endpoint
// optional — resolved from the saved profile or falling back to
// defaultRegistryEndpoint), or an interactive bubbletea wizard when no
// input flags are supplied on a TTY.
func NewCmdConfigure(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &configureOptions{
		ExpiresInDays: defaultExpiresInDays,
	}

	cmd := &cobra.Command{
		Use:   "configure",
		Short: registryConfigureShort,
		Long: cmdutil.LongDesc(`
			Save Verda Container Registry credentials to ~/.verda/credentials
			(override with --credentials-file or VERDA_REGISTRY_CREDENTIALS_FILE).
			Stored with verda_registry_ prefixed keys alongside API and S3 keys;
			other keys in the profile are preserved.

			Three ways to provide credentials:
			  • interactive — run with no flags on a terminal; the wizard guides you,
			    including where to find the credential in the web UI.
			  • --paste — paste the web UI's "Registry authentication command" (the
			    docker login … line); the host is auto-detected.
			  • --username + --password-stdin [--endpoint] — Docker-style; --endpoint
			    defaults to the profile's saved host, else vccr.io.

			Find the credential in the web UI under: select project → Credentials →
			Create credentials.
		`),
		Example: cmdutil.Examples(`
			# Interactive wizard (recommended for first-time setup)
			verda registry configure

			# Paste the web UI's "Registry authentication command"
			verda registry configure --paste "docker login -u vcr-abc+cli -p s3cret vccr.io"

			# Username + secret on stdin (--endpoint defaults to vccr.io)
			echo -n "$SECRET" | verda registry configure --username vcr-abc+cli --password-stdin
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigure(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name (default: active profile)")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared credentials file")
	flags.StringVar(&opts.Username, "username", "", "Registry username (vcr-<project-id>+<name>)")
	flags.BoolVar(&opts.PasswordStdin, "password-stdin", false, "Read the registry secret from stdin")
	flags.StringVar(&opts.Endpoint, "endpoint", "",
		"Registry host (e.g. \"vccr.io\"). Defaults to the saved endpoint for this profile, "+
			"or \""+defaultRegistryEndpoint+"\" on production.")
	flags.IntVar(&opts.ExpiresInDays, "expires-in", opts.ExpiresInDays, "Days from now until the credentials expire")
	flags.StringVar(&opts.Paste, "paste", "", "Full `docker login ...` command to parse (alternative to --username/--password-stdin)")
	flags.BoolVar(&opts.DockerConfig, "docker-config", false, "Also write ~/.docker/config.json (same merge as `verda registry configure-docker`)")

	return cmd
}

// runConfigure is the RunE body, split out for testability.
func runConfigure(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *configureOptions) error {
	// If no non-interactive input mode was supplied and we're not in agent
	// mode, drive the interactive wizard to populate opts.Paste (plus the
	// derived Username/Endpoint) and opts.ExpiresInDays / opts.DockerConfig
	// before falling through to the flag-driven resolution path below.
	if shouldRunConfigureWizard(f, opts) {
		// The docker-login string lives several clicks deep in the web UI;
		// tell the user exactly where to find it before the paste prompt.
		printConfigureIntro(ioStreams)
		flow := buildConfigureFlow(opts)
		engine := wizard.NewEngine(f.Prompter(), f.Status(),
			wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
		if err := engine.Run(cmd.Context(), flow); err != nil {
			return err
		}
	}

	// Resolve the target profile now that the wizard (if any) has run: an
	// explicit --profile or the picker's choice is already on opts.Profile;
	// otherwise fall back to the active profile so configure writes where the
	// read commands will look.
	opts.Profile = resolveProfile(opts.Profile)

	creds, err := resolveRegistryInputs(cmd, f, ioStreams, opts)
	if err != nil {
		return err
	}

	path := credentialsFilePath(opts.CredentialsFile)

	// The registry secret is write-once (the API never returns it), so
	// replacing a profile's existing credentials is irreversible. Confirm on a
	// TTY; proceed (with a note) for scripted rotation.
	proceed, err := confirmOverwriteIfConfigured(cmd.Context(), f, ioStreams, opts.Profile, path)
	if err != nil {
		return err
	}
	if !proceed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}

	if err := options.WriteRegistryCredentialsToProfile(path, opts.Profile, creds); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Registry credentials saved to profile %q in %s\n", opts.Profile, path)
	if !creds.ExpiresAt.IsZero() {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Credentials expire at %s (in %d days)\n",
			creds.ExpiresAt.Format(time.RFC3339), opts.ExpiresInDays)
	}

	if opts.DockerConfig {
		writeDockerConfigForConfigure(ioStreams, creds)
	}
	return nil
}

// printConfigureIntro tells the user exactly where the docker-login string
// lives in the Verda web UI before the wizard prompts for it. The path is
// several clicks deep, so new users otherwise can't find the "Registry
// authentication command" the paste step asks for. Mirrors objectstorage's
// printConfigureIntro. Goes to ErrOut so stdout stays clean.
func printConfigureIntro(ioStreams cmdutil.IOStreams) {
	w := ioStreams.ErrOut
	_, _ = fmt.Fprintln(w, "\n  Create a Container Registry credential in the Verda dashboard first:")
	_, _ = fmt.Fprintln(w, "    1. Open the dashboard and select your project (skipped if you have only one).")
	_, _ = fmt.Fprintln(w, "    2. Go to Credentials → Create credentials.")
	_, _ = fmt.Fprintln(w, "    3. Provider: Verda. Enter a name and an expiry (Label, in days),")
	_, _ = fmt.Fprintln(w, "       then click \"Create credentials\".")
	_, _ = fmt.Fprintln(w, "    4. In the dialog, copy either the full \"Registry authentication command\"")
	_, _ = fmt.Fprintln(w, "       (the `docker login …` line) or the \"Full credentials name\" + \"Secret\".")
	_, _ = fmt.Fprintln(w, "       The secret is shown only once.")
	_, _ = fmt.Fprintln(w, "\n  The wizard will ask which of those you have.")
	_, _ = fmt.Fprintln(w)
}

// confirmOverwriteIfConfigured guards the irreversible replace of a profile's
// existing registry credentials. Returns (proceed, err):
//
//   - profile has no registry creds yet → (true, nil); no prompt.
//   - interactive TTY                   → prompt to replace; decline → (false, nil).
//   - agent / non-TTY                   → (true, nil); rotation is the explicit
//     intent and scripts must not block. A non-agent run still emits a one-line
//     "Replacing…" note so the overwrite isn't silent.
//
// A failed load is treated as "nothing to overwrite" (true, nil): the worst
// case is we skip a warning, never that we block a legitimate first-time write.
func confirmOverwriteIfConfigured(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, profile, path string) (bool, error) {
	existing, err := options.LoadRegistryCredentialsForProfile(path, profile)
	if err != nil || existing == nil || !existing.HasCredentials() {
		return true, nil //nolint:nilerr // intentional: load failure = nothing to overwrite, proceed
	}

	detail := existing.Username
	if !existing.ExpiresAt.IsZero() {
		detail = fmt.Sprintf("%s, expires %s", existing.Username, existing.ExpiresAt.Format("2006-01-02"))
	}

	if f.AgentMode() || !isTerminalFn(ioStreams.Out) {
		if !f.AgentMode() {
			_, _ = fmt.Fprintf(ioStreams.ErrOut,
				"Replacing existing registry credentials in profile %q (%s).\n", profile, detail)
		}
		return true, nil
	}

	confirmed, err := f.Prompter().Confirm(ctx,
		fmt.Sprintf("Profile %q already has registry credentials (%s). Replace them?", profile, detail))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	return confirmed, nil
}

// writeDockerConfigForConfigure honors the --docker-config flag (and the
// wizard's "Also write ~/.docker/config.json?" step) by merging the just-saved
// credentials into the Docker config via the same writer `registry configure-docker`
// uses. A failure here is non-fatal: the Verda credentials are already saved,
// so we warn and point at `registry configure-docker` for a retry rather than failing the
// whole command.
func writeDockerConfigForConfigure(ioStreams cmdutil.IOStreams, creds *options.RegistryCredentials) {
	dockerPath, err := resolveDockerConfigPath("")
	if err == nil {
		err = writeDockerLogin(dockerPath, creds)
	}
	if err != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut,
			"warning: could not write Docker config: %v\nRun `verda registry configure-docker` to retry.\n", err)
		return
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut,
		"Also wrote %s for use with docker pull / docker-compose / helm.\n", dockerPath)
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

	case strings.TrimSpace(opts.Username) != "" && opts.Secret != "":
		// Wizard manual-entry path: the user typed the full credential name and
		// secret (the secret never touches stdin). The web UI's name+secret
		// fields don't carry a host, so derive it the same way the flag path
		// does — the profile's saved endpoint, else the production base
		// (vccr.io) — and parse the project id out of the credential name.
		projectID, err := projectIDFromUsername(opts.Username)
		if err != nil {
			return nil, cmdutil.UsageErrorf(cmd, "%s", err.Error())
		}
		endpoint, source := resolveEndpointForFlags(opts)
		if source != endpointSourceFlag && !f.AgentMode() {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Using registry endpoint %q (%s).\n", endpoint, source)
		}
		return &options.RegistryCredentials{
			Username:  opts.Username,
			Secret:    opts.Secret,
			Endpoint:  endpoint,
			ProjectID: projectID,
			ExpiresAt: expiresAt,
		}, nil

	case strings.TrimSpace(opts.Username) != "" && opts.PasswordStdin:
		endpoint, source := resolveEndpointForFlags(opts)
		if endpoint == "" {
			return nil, cmdutil.UsageErrorf(cmd,
				"--endpoint is required with --username/--password-stdin.\n"+
					"The endpoint appears in the 'Registry authentication command' field of the Verda web UI,\n"+
					"e.g. `docker login -u ... -p ... vccr.io` → --endpoint vccr.io.\n"+
					"Or use --paste to supply the full command in one go.")
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
		// Surface endpoint provenance when it wasn't set by the user, so a
		// silent production-default or profile-reuse doesn't surprise
		// anyone running on staging. Kept to stderr + non-agent mode so
		// JSON consumers and scripts piping stdout stay clean.
		if source != endpointSourceFlag && !f.AgentMode() {
			_, _ = fmt.Fprintf(ioStreams.ErrOut,
				"Using registry endpoint %q (%s). Pass --endpoint to override.\n",
				endpoint, source)
		}
		return &options.RegistryCredentials{
			Username:  opts.Username,
			Secret:    secret,
			Endpoint:  endpoint,
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

// Endpoint source labels used by resolveEndpointForFlags to explain (in
// stderr) why a particular endpoint was chosen when --endpoint was absent.
const (
	endpointSourceFlag    = "flag"
	endpointSourceSaved   = "saved in profile"
	endpointSourceDefault = "production default"
)

// resolveEndpointForFlags decides which registry endpoint to use when the
// caller drove the --username/--password-stdin path. Resolution order:
//
//  1. explicit --endpoint flag            → as-is
//  2. saved endpoint for opts.Profile     → reused (credential rotation)
//  3. defaultRegistryEndpoint (vccr.io)   → production fallback
//
// The returned source tag is used by the caller to explain non-explicit
// choices on stderr so staging users don't silently get a vccr.io default.
// Returns ("", "") only if something catastrophic prevents all three paths
// from yielding a value — currently unreachable but left for safety.
func resolveEndpointForFlags(opts *configureOptions) (endpoint, source string) {
	if e := strings.TrimSpace(opts.Endpoint); e != "" {
		return e, endpointSourceFlag
	}
	if e := loadSavedEndpoint(opts.Profile, opts.CredentialsFile); e != "" {
		return e, endpointSourceSaved
	}
	return defaultRegistryEndpoint, endpointSourceDefault
}

// loadSavedEndpoint returns the previously-saved verda_registry_endpoint
// for the given profile, or "" if none is set / the file is missing /
// the profile section doesn't exist. Errors are intentionally swallowed:
// the caller treats absence the same as a missing key and falls through
// to defaultRegistryEndpoint.
func loadSavedEndpoint(profile, credentialsFile string) string {
	if strings.TrimSpace(profile) == "" {
		profile = defaultProfileName
	}
	path := credentialsFilePath(credentialsFile)
	if path == "" {
		return ""
	}
	creds, err := options.LoadRegistryCredentialsForProfile(path, profile)
	if err != nil || creds == nil {
		return ""
	}
	return strings.TrimSpace(creds.Endpoint)
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
