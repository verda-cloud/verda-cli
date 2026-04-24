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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// lsOptions bundles flag state for `verda registry ls`.
type lsOptions struct {
	Profile         string
	CredentialsFile string
}

// lsPayload is the top-level structured payload for --output json/yaml.
// Wrapping `Repositories` in an object (rather than emitting a bare list)
// keeps room for future top-level metadata (project, total, pagination)
// without a breaking schema change.
type lsPayload struct {
	Project      string           `json:"project" yaml:"project"`
	Repositories []RepositoryInfo `json:"repositories" yaml:"repositories"`
}

// NewCmdLs creates the `verda registry ls` command.
//
// ls lists every repository in the active Verda project using Harbor's
// REST API. On a TTY, selecting a repository drills into its artifact
// list (digest / tags / size / pushed / pulled) — mirroring the Harbor
// UI's "Image list" view. When piped or redirected (or with -o json /
// yaml), ls prints a single non-interactive table/document.
func NewCmdLs(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &lsOptions{Profile: defaultProfileName}

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List repositories in the active Verda project",
		Long: cmdutil.LongDesc(`
			List every repository in the Verda project associated with the
			active registry credentials. Repositories are resolved via
			Harbor's REST API, not the Docker Registry v2 catalog endpoint —
			the catalog endpoint is admin-only on Harbor and is not
			accessible to the project-scoped robot accounts VCR issues.

			When run on a terminal, selecting a repository drills into its
			per-artifact details (digest, tags, size, push/pull times).
			When piped to a file or another command (or when -o json/yaml
			is set), ls prints a single non-interactive document.
		`),
		Example: cmdutil.Examples(`
			# Interactive: pick a repo to see its image list
			verda registry ls

			# Non-interactive table (piping suppresses the picker)
			verda registry ls | less

			# JSON output for scripts
			verda registry ls -o json

			# Use a non-default credentials profile
			verda registry ls --profile staging
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared Verda credentials file")

	return cmd
}

// runLs is the RunE body, split out for testability. Pre-flight parity
// with tags/push/copy: load creds, check expiry, build the client through
// the swap point, run under the factory's timeout.
func runLs(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *lsOptions) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Any "no usable creds" shape collapses to registry_not_configured
		// (same pattern as tags/push/copy).
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}
	if creds.ProjectID == "" {
		return &cmdutil.AgentError{
			Code: kindRegistryNotConfigured,
			Message: "Active credential has no project id. Re-run `verda registry configure` " +
				"with the docker-login string from the Verda UI.",
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	lister := buildHarborLister(creds, RetryConfig{})

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	sp := startLsSpinner(ctx, f, creds.Endpoint)
	repos, err := lister.ListRepositories(ctx, creds.ProjectID)
	stopSpinner(sp)
	if err != nil {
		return translateErrorWithExpiry(err, creds)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
		fmt.Sprintf("API response: %d repository(ies) for project %s:", len(repos), creds.ProjectID), repos)

	// Deterministic output: sort by display name.
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })

	payload := lsPayload{Project: creds.ProjectID, Repositories: repos}

	// Structured output: emit and return — never enter the picker.
	if isStructuredFormat(f.OutputFormat()) {
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			return werr
		}
	}

	// No repositories: print the friendly empty-state message in both
	// TTY and non-TTY branches. The picker would only offer "Exit"
	// anyway, so skipping it is the least surprising behavior.
	if len(repos) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "No repositories found in project %s.\n", payload.Project)
		return nil
	}

	// Non-TTY: print the plain table and return. This is what scripts,
	// piped output, and CI all hit — the path must stay deterministic
	// and line-oriented.
	if !isTerminalFn(ioStreams.Out) {
		renderLsHuman(ioStreams, payload)
		return nil
	}

	// TTY: interactive drill-down. Mirrors cmd/vm/list.go — use the
	// factory's prompter so agent mode / automation still gets a
	// structured AgentError instead of a blocked terminal.
	return runLsInteractive(ctx, f, ioStreams, lister, payload)
}

// runLsInteractive loops: render a single-line summary per repo, let the
// user pick one, then render that repo's artifact card. Selecting Exit
// (or Ctrl-C on the picker) returns nil — same contract as `vm ls`.
func runLsInteractive(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, payload lsPayload) error {
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %d repository(ies) in project %s\n\n",
		len(payload.Repositories), payload.Project)

	prompter := f.Prompter()
	labels := make([]string, 0, len(payload.Repositories)+1)
	for i := range payload.Repositories {
		labels = append(labels, formatRepoRow(&payload.Repositories[i]))
	}
	labels = append(labels, "Exit")

	for {
		idx, err := prompter.Select(ctx, "Select repository (type to filter)", labels)
		if err != nil {
			// Prompter-layer cancellation (Ctrl-C, ESC) returns a
			// sentinel error; vm list treats it as a clean exit.
			return nil //nolint:nilerr // intentional: prompter cancel is a clean exit
		}
		if idx == len(payload.Repositories) { // "Exit"
			return nil
		}

		repo := payload.Repositories[idx]
		arts, artErr := lister.ListArtifacts(ctx, payload.Project, repo.Name)
		if artErr != nil {
			// Keep the loop alive so one flaky repo doesn't kick the
			// user out of the picker (matches vm list's fetch-details
			// error handling).
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", artErr)
			continue
		}
		renderRepoArtifacts(ioStreams, repo, arts)
	}
}

// startLsSpinner starts the listing spinner (nil-safe via stopSpinner).
// Message matches tags.go's "Listing ..." phrasing.
func startLsSpinner(ctx context.Context, f cmdutil.Factory, host string) interface{ Stop(string) } {
	status := f.Status()
	if status == nil {
		return nil
	}
	sp, _ := status.Spinner(ctx, fmt.Sprintf("Listing repositories on %s...", host))
	return sp
}

// renderLsHuman prints the human-readable table to ioStreams.Out. Columns
// mirror tags.go's style for cross-command visual consistency. ARTIFACTS
// and PULLS are right-aligned; UPDATED is an RFC3339-ish short form.
func renderLsHuman(ioStreams cmdutil.IOStreams, payload lsPayload) {
	if len(payload.Repositories) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "No repositories found in project %s.\n", payload.Project)
		return
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d repository(ies) in project %s\n\n",
		len(payload.Repositories), payload.Project)
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-48s    %9s    %9s    %s\n",
		"REPOSITORY", "ARTIFACTS", "PULLS", "UPDATED")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-48s    %9s    %9s    %s\n",
		"----------", "---------", "-----", "-------")

	for _, r := range payload.Repositories {
		updated := "--"
		if !r.UpdateTime.IsZero() {
			updated = r.UpdateTime.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-48s    %9s    %9s    %s\n",
			r.Name,
			strconv.FormatInt(r.ArtifactCount, 10),
			strconv.FormatInt(r.PullCount, 10),
			updated,
		)
	}
}

// formatRepoRow builds the single-line label shown in the repo picker.
// It's intentionally narrow-ish so it fits in 80 columns with the picker
// chrome, and right-aligns the numeric columns so a column of repos
// reads like a table.
func formatRepoRow(r *RepositoryInfo) string {
	updated := "--"
	if !r.UpdateTime.IsZero() {
		updated = r.UpdateTime.UTC().Format("2006-01-02")
	}
	name := r.Name
	if len(name) > 48 {
		name = name[:45] + "..."
	}
	return fmt.Sprintf("%-48s  %4d artifact(s)  %6d pull(s)  %s",
		name, r.ArtifactCount, r.PullCount, updated)
}

// renderRepoArtifacts prints the per-repo detail card: header + image
// list. Columns mirror Harbor's UI ("Image list" section): DIGEST / TAGS
// / SIZE / PUSHED / PULLED. PullTime=zero means "never pulled" and is
// rendered as "--" (Harbor shows an em-dash for the same state).
func renderRepoArtifacts(ioStreams cmdutil.IOStreams, repo RepositoryInfo, arts []ArtifactInfo) {
	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s\n", repo.Name)
	if repo.FullName != "" && repo.FullName != repo.Name {
		_, _ = fmt.Fprintf(ioStreams.Out, "  (full path: %s)\n", repo.FullName)
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "  %d artifact(s), %d pull(s)\n\n",
		repo.ArtifactCount, repo.PullCount)

	if len(arts) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "  No artifacts.")
		return
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %-22s  %-30s  %10s  %-19s  %-19s\n",
		"DIGEST", "TAGS", "SIZE", "PUSHED", "PULLED")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-22s  %-30s  %10s  %-19s  %-19s\n",
		"------", "----", "----", "------", "------")

	for i := range arts {
		a := &arts[i]
		tags := strings.Join(a.Tags, ", ")
		if tags == "" {
			tags = untaggedLabel
		}
		if len(tags) > 30 {
			tags = tags[:27] + "..."
		}
		pushed := "--"
		if !a.PushTime.IsZero() {
			pushed = a.PushTime.UTC().Format("2006-01-02 15:04:05")
		}
		pulled := "--"
		if !a.PullTime.IsZero() {
			pulled = a.PullTime.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-22s  %-30s  %10s  %-19s  %-19s\n",
			shortDigest(a.Digest),
			tags,
			formatBytes(a.Size),
			pushed,
			pulled,
		)
	}
	_, _ = fmt.Fprintln(ioStreams.Out)
}

// shortDigest returns the first 19 chars of a digest ("sha256:abcdef012")
// — enough to be uniquely identifying in a small project while keeping
// the column narrow. Docker / ggcr both accept the short form when used
// as a pull reference.
func shortDigest(d string) string {
	if len(d) <= 22 {
		return d
	}
	return d[:22]
}
