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
	"sort"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
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
	opts := &lsOptions{}

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
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read (default: active profile)")
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
	//
	// Pass cmd.Context() (NOT the per-request timeout ctx used for the
	// listing above): the interactive loop includes user think-time, so a
	// 30s --timeout must not cancel a prompt mid-browse. Ctrl+C is the stop
	// signal; per-call API timeouts inside the loop are a future refinement.
	return runLsInteractive(cmd.Context(), f, ioStreams, lister, payload, creds)
}

// runLsInteractive loops: render a single-line summary per repo, let the
// user pick one, then render that repo's artifact card. Selecting Exit
// (or Ctrl-C on the picker) returns nil — same contract as `vm ls`.
func runLsInteractive(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, payload lsPayload, creds *options.RegistryCredentials) error {
	host := creds.Endpoint
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %d repository(ies) in project %s\n\n",
		len(payload.Repositories), payload.Project)

	prompter := f.Prompter()
	labels := make([]string, 0, len(payload.Repositories)+1)
	for i := range payload.Repositories {
		labels = append(labels, formatRepoRow(&payload.Repositories[i]))
	}
	labels = append(labels, "Exit")

	for {
		idx, err := prompter.Select(ctx, registryBreadcrumb(host, ""), labels, tui.WithShowHints(true))
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
		switch {
		case artErr == nil:
			// Repo action menu: copy a pull URL or delete image(s).
			exit, perr := runRepoActions(ctx, f, ioStreams, lister, creds, repo, arts)
			if perr != nil {
				return perr
			}
			if exit {
				return nil
			}
		case isAccessDenied(artErr):
			// Harbor's artifact API is denied for this credential, but the
			// Docker v2 tag list (what `registry tags` uses) usually isn't —
			// fall back to it so the tag picker / pull URLs still work. Delete
			// stays unavailable here (it goes through the same denied Harbor
			// API). If even the v2 list is denied, show just the repo path.
			base := repoPullBase(host, payload.Project, &repo)
			if tags := tagsViaRegistry(ctx, creds, repo); len(tags) > 0 {
				_, _ = fmt.Fprintln(ioStreams.ErrOut,
					"  Listing tags via the registry API (this credential can't read Harbor image details; sizes/dates omitted).")
				exit, perr := runTagPicker(ctx, f, ioStreams, repo.Name, base, tagEntriesFromNames(tags))
				if perr != nil {
					return perr
				}
				if exit {
					return nil
				}
				continue
			}
			renderRepoPullPath(ioStreams, host, payload.Project, repo)
			exit, perr := cmdutil.PromptBackOrExit(ctx, prompter)
			if perr != nil {
				return perr
			}
			if exit {
				return nil
			}
		default:
			// Transient (e.g. 5xx): surface it and keep the picker open so one
			// flaky repo doesn't kick the user out (matches vm list's
			// fetch-details error handling).
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", artErr)
		}
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
	return fmt.Sprintf("%s %-48s  %4d artifact(s)  %6d pull(s)  %s",
		repoGlyph, name, r.ArtifactCount, r.PullCount, updated)
}

// runRepoActions is the per-repo action menu shown after a repo is selected in
// the interactive browser: copy a pull URL (tag picker) or delete image(s).
// The delete path reuses runDeleteImagesInteractive so there is exactly one
// destructive flow (same multi-select + red-warning confirm as
// `verda registry delete`). Returns exit=true only on Ctrl+C; Esc / "Back"
// returns to the repository list.
func runRepoActions(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	lister RepositoryLister, creds *options.RegistryCredentials, repo RepositoryInfo, arts []ArtifactInfo) (bool, error) {
	const (
		actPull = iota
		actDelete
		actBack
	)
	choices := []string{
		"Get pull URL (pick a tag)",
		"Delete image(s)…",
		"← Back to repositories",
	}

	prompter := f.Prompter()
	title := repo.Name + " — choose an action"
	for {
		idx, err := prompter.Select(ctx, title, choices, tui.WithShowHints(true))
		if err != nil {
			if cmdutil.IsPromptInterrupt(err) {
				return true, nil // Ctrl+C quits the command
			}
			return false, nil // Esc → back to the repository list
		}
		switch idx {
		case actPull:
			base := repoPullBase(creds.Endpoint, creds.ProjectID, &repo)
			exit, perr := runTagPicker(ctx, f, ioStreams, repo.Name, base, buildTagEntries(arts))
			if perr != nil {
				return false, perr
			}
			if exit {
				return true, nil
			}
		case actDelete:
			// Same flow `verda registry delete` uses: multi-select + confirm.
			if derr := runDeleteImagesInteractive(ctx, f, ioStreams, lister, creds, repo); derr != nil {
				return false, derr
			}
			// Refresh artifacts so a subsequent "Get pull URL" reflects deletes.
			if refreshed, rerr := lister.ListArtifacts(ctx, creds.ProjectID, repo.Name); rerr == nil {
				arts = refreshed
			}
		case actBack:
			return false, nil
		}
	}
}

// tagEntry is one row in the drill-down tag picker. suffix is appended to the
// repo's pull base to form the full reference: ":<tag>" for tagged artifacts,
// "@<digest>" for untagged ones (SBOMs, referrers).
type tagEntry struct {
	label  string
	suffix string
}

// buildTagEntries flattens a repo's artifacts into one picker row per tag,
// newest first (by artifact push time). Untagged artifacts become a single
// "<untagged> <short-digest>" row that resolves to an @digest reference.
func buildTagEntries(arts []ArtifactInfo) []tagEntry {
	sorted := make([]ArtifactInfo, len(arts))
	copy(sorted, arts)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].PushTime.After(sorted[j].PushTime)
	})

	entries := make([]tagEntry, 0, len(sorted))
	for i := range sorted {
		a := &sorted[i]
		size := formatBytes(a.Size)
		age := formatAgo(a.PushTime, nil)
		if len(a.Tags) == 0 {
			entries = append(entries, tagEntry{
				label:  fmt.Sprintf("%-30s  %10s  %s", untaggedLabel+" "+shortDigest(a.Digest), size, age),
				suffix: "@" + a.Digest,
			})
			continue
		}
		for _, t := range a.Tags {
			entries = append(entries, tagEntry{
				label:  fmt.Sprintf("%-30s  %10s  %s", t, size, age),
				suffix: ":" + t,
			})
		}
	}
	return entries
}

// tagEntriesFromNames builds picker rows from bare tag names — used by the
// access-denied fallback, where only the Docker v2 tag list is available (no
// per-artifact size/push-time, so labels are just the tag).
func tagEntriesFromNames(tags []string) []tagEntry {
	entries := make([]tagEntry, 0, len(tags))
	for _, t := range tags {
		entries = append(entries, tagEntry{label: t, suffix: ":" + t})
	}
	return entries
}

// runTagPicker drives the repo drill-down over a prebuilt entry list: filter
// (type-to-filter on the Select) and pick a tag, then print its full,
// copy-pasteable pull reference (base + suffix) and quit — getting the URL is
// the goal, so once it's on stdout the user can copy it and move on rather than
// being parked back in the picker. Returns quit=true when a tag was picked or
// Ctrl+C; Esc or the trailing "Back" row returns to the previous menu.
func runTagPicker(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams,
	repoName, base string, entries []tagEntry) (bool, error) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s\n  No tags.\n  Pull:\n    %s\n\n", repoName, base)
		return false, nil
	}

	const backLabel = "← Back"
	labels := make([]string, 0, len(entries)+1)
	for i := range entries {
		labels = append(labels, entries[i].label)
	}
	labels = append(labels, backLabel)

	prompter := f.Prompter()
	title := repoName + " — select a tag (type to filter)"
	idx, err := prompter.Select(ctx, title, labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptInterrupt(err) {
			return true, nil // Ctrl+C quits the whole command
		}
		return false, nil // Esc → back to the previous menu
	}
	if idx == len(entries) { // "← Back"
		return false, nil
	}
	// The chosen tag's full pull reference, on stdout so it's clean to copy.
	// Then quit: the user got what they came for.
	_, _ = fmt.Fprintf(ioStreams.Out, "  %s%s\n", base, entries[idx].suffix)
	return true, nil
}

// renderRepoPullPath prints just the repository's pull reference, used when the
// credential can list repositories but not their artifacts (Harbor 403 on the
// artifacts endpoint). It surfaces the one actionable thing — the path the user
// can `docker pull` — instead of an error. The explanatory note goes to ErrOut
// so the reference on stdout stays clean to copy.
func renderRepoPullPath(ioStreams cmdutil.IOStreams, host, project string, repo RepositoryInfo) {
	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s\n", repo.Name)
	_, _ = fmt.Fprintln(ioStreams.ErrOut,
		"  Tag details need a credential with pull permission. You can still pull the repository:")
	_, _ = fmt.Fprintf(ioStreams.Out, "  Pull:\n    %s\n\n", repoPullBase(host, project, &repo))
}

// repoPullBase builds the registry pull reference for a repository:
// "<host>/<project>/<repo>". It prefers the Harbor-supplied FullName
// (already "<project>/<repo>") and falls back to project + Name. Host defaults
// to the production endpoint for legacy creds that never stored one. Append
// ":<tag>" or "@<digest>" for a specific artifact.
func repoPullBase(host, project string, repo *RepositoryInfo) string {
	full := repo.FullName
	if full == "" {
		full = project + "/" + repo.Name
	}
	if host == "" {
		host = defaultRegistryEndpoint
	}
	return host + "/" + full
}

// tagsViaRegistry falls back to the Docker Registry v2 tag list (the API
// `registry tags` uses) when Harbor's artifact API is denied. Many credentials
// can pull/list over v2 even without the Harbor project permission the
// artifacts endpoint requires. Returns nil on any error so the caller degrades
// to showing just the repository path.
func tagsViaRegistry(ctx context.Context, creds *options.RegistryCredentials, repo RepositoryInfo) []string {
	full := repo.FullName
	if full == "" {
		full = creds.ProjectID + "/" + repo.Name
	}
	tags, err := buildClient(creds, RetryConfig{}).Tags(ctx, full)
	if err != nil {
		return nil
	}
	return tags
}

// isAccessDenied reports whether err is a translated registry_access_denied
// AgentError (Harbor 403) — a permission state, not a transient failure.
func isAccessDenied(err error) bool {
	var ae *cmdutil.AgentError
	return errors.As(err, &ae) && ae.Code == kindRegistryAccessDenied
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
