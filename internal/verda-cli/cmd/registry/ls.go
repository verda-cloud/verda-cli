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
	"strconv"
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// defaultLsLimit caps the number of per-repo metadata lookups (Tags) that
// `verda registry ls` performs unless --all is set or --limit is 0 (unlimited).
const defaultLsLimit = 50

// lsOptions bundles flag state for `verda registry ls`.
type lsOptions struct {
	Profile         string
	CredentialsFile string
	Limit           int
	All             bool
}

// repoRow is the per-repository row emitted in structured (JSON/YAML) output.
//
// LastPushAt is intentionally a pointer so the zero value marshals as JSON
// null / is omitted from YAML when we don't have a timestamp. For v1 we
// always leave it nil (best-effort): resolving the push timestamp requires
// fetching the manifest config blob for every newest tag, which doubles the
// request count for large catalogs. Users can run `verda registry tags
// <repo>` for detailed per-tag metadata.
//
// TODO: populate LastPushAt by resolving manifest.Config.Digest ->
// remote.Image(ref).ConfigFile() -> .Created.Time in a follow-up task.
type repoRow struct {
	Repository string     `json:"repository" yaml:"repository"`
	TagCount   int        `json:"tag_count" yaml:"tag_count"`
	LastPushAt *time.Time `json:"last_push_at,omitempty" yaml:"last_push_at,omitempty"`
}

// lsPayload is the top-level structured payload.
type lsPayload struct {
	Repositories []repoRow `json:"repositories" yaml:"repositories"`
}

// NewCmdLs creates the `verda registry ls` command.
//
// ls shows every repository visible to the active registry credentials
// (via the Docker Registry v2 _catalog endpoint) plus a per-repo tag count.
// The catalog call is always made in full; per-repo metadata lookups are
// capped by --limit unless --all is set. Repositories past the metadata
// cap are still listed by name, with "--" under the TAGS column.
//
// --limit 0 means unlimited (Unix convention). --all is equivalent to
// --limit 0; the two flags together are redundant but not an error.
func NewCmdLs(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &lsOptions{
		Profile: defaultProfileName,
		Limit:   defaultLsLimit,
	}

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List repositories in the container registry",
		Long: cmdutil.LongDesc(`
			List all repositories visible to the active Verda Container
			Registry credentials, with a per-repo tag count.

			The full catalog is always returned. Per-repository metadata
			lookups (tag counts) are capped by --limit to keep response
			time bounded on large registries. Repositories past the cap
			are listed by name with "--" in the TAGS column.

			Pass --all to run metadata lookups for every repository
			regardless of --limit. --limit 0 is equivalent to --all.

			The "LAST PUSH" column is reserved for a future release and
			currently prints "--" for every row.
		`),
		Example: cmdutil.Examples(`
			# List repositories (first 50 with tag counts)
			verda registry ls

			# Raise the metadata cap
			verda registry ls --limit 200

			# Metadata for every repository (slower on large registries)
			verda registry ls --all

			# JSON output for scripts
			verda registry ls -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared Verda credentials file")
	flags.IntVar(&opts.Limit, "limit", opts.Limit, "Cap on per-repo metadata lookups (0 = unlimited)")
	flags.BoolVar(&opts.All, "all", false, "Run metadata lookups for every repository (overrides --limit)")

	return cmd
}

// runLs is the RunE body, split out for testability.
func runLs(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *lsOptions) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Mirror login's single-error-surface pattern: any "no usable creds"
		// shape collapses to registry_not_configured.
		_ = err
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}

	// ls does not yet expose a --retries flag; pass an empty RetryConfig
	// so the ggcr default transport is used (retries disabled).
	reg := buildClient(creds, RetryConfig{})

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Listing repositories...")
	}

	repos, catErr := reg.Catalog(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if catErr != nil {
		return translateErrorWithExpiry(catErr, creds)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
		fmt.Sprintf("API response: %d repo(s):", len(repos)), repos)

	// Decide how many repos get metadata lookups. --all or --limit 0 mean
	// "everything"; otherwise cap at opts.Limit.
	metadataCap := opts.Limit
	if opts.All || opts.Limit <= 0 {
		metadataCap = len(repos)
	}

	payload := lsPayload{Repositories: make([]repoRow, 0, len(repos))}
	for i, repo := range repos {
		row := repoRow{Repository: repo}
		if i < metadataCap {
			// Serial per-repo lookups keep the code simple and the spinner
			// honest; concurrency is a future optimization.
			tags, tagsErr := reg.Tags(ctx, repo)
			if tagsErr != nil {
				return translateErrorWithExpiry(tagsErr, creds)
			}
			row.TagCount = len(tags)
		} else {
			// Sentinel: negative TagCount encodes "not looked up" so the
			// human renderer knows to print "--" instead of "0".
			row.TagCount = -1
		}
		// LastPushAt stays nil — see repoRow doc comment + TODO.
		payload.Repositories = append(payload.Repositories, row)
	}

	if isStructuredFormat(f.OutputFormat()) {
		// Strip the sentinel so TagCount on not-looked-up rows is 0 in
		// structured output (matching the AgentError-style convention of
		// never leaking internal negative-sentinel values).
		for i := range payload.Repositories {
			if payload.Repositories[i].TagCount < 0 {
				payload.Repositories[i].TagCount = 0
			}
		}
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			return werr
		}
	}

	renderLsHuman(ioStreams, payload)
	return nil
}

// renderLsHuman prints the human-readable table to ioStreams.Out. Empty
// catalogs render as a single "No repositories found." line.
func renderLsHuman(ioStreams cmdutil.IOStreams, payload lsPayload) {
	if len(payload.Repositories) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No repositories found.")
		return
	}

	// Match s3 ls's column style: two-space left pad, %-N column widths,
	// no alternating-row colors. Keep columns aligned with a divider row.
	_, _ = fmt.Fprintf(ioStreams.Out, "  %d repository(s) found\n\n", len(payload.Repositories))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-40s    %-6s    %s\n", "REPOSITORY", "TAGS", "LAST PUSH")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-40s    %-6s    %s\n", "----------", "----", "---------")

	for _, r := range payload.Repositories {
		tagStr := "--"
		if r.TagCount >= 0 {
			tagStr = strconv.Itoa(r.TagCount)
		}
		lastPush := "--"
		if r.LastPushAt != nil {
			lastPush = r.LastPushAt.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-40s    %-6s    %s\n", r.Repository, tagStr, lastPush)
	}
}

// isStructuredFormat reports whether the output format is a machine-readable
// one that must not be interleaved with human lines. Mirrors s3's
// isStructured so registry commands stay consistent without a cross-package
// import cycle.
func isStructuredFormat(format string) bool {
	return format == "json" || format == "yaml"
}
