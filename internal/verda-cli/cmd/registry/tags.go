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
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// defaultTagsLimit caps the number of per-tag HEAD lookups `verda registry
// tags <repo>` performs unless --all is set or --limit is 0 (unlimited).
const defaultTagsLimit = 50

// tagsOptions bundles flag state for `verda registry tags`.
type tagsOptions struct {
	Profile         string
	CredentialsFile string
	Limit           int
	All             bool
}

// tagRow is the per-tag row emitted in structured (JSON/YAML) output.
//
// PushedAt is intentionally a pointer so a missing timestamp marshals as JSON
// null / is omitted from YAML. For v1 we always leave it nil (best-effort):
// resolving the push timestamp requires fetching the manifest config blob,
// which doubles the request count. Mirror the ls-side TODO.
//
// TODO: populate PushedAt by resolving manifest.Config.Digest ->
// remote.Image(ref).ConfigFile() -> .Created.Time in a follow-up task.
type tagRow struct {
	Tag      string     `json:"tag" yaml:"tag"`
	Digest   string     `json:"digest,omitempty" yaml:"digest,omitempty"`
	Size     int64      `json:"size" yaml:"size"`
	PushedAt *time.Time `json:"pushed_at,omitempty" yaml:"pushed_at,omitempty"`
}

// tagsPayload is the top-level structured payload.
type tagsPayload struct {
	Repository string   `json:"repository" yaml:"repository"`
	Tags       []tagRow `json:"tags" yaml:"tags"`
}

// NewCmdTags creates the `verda registry tags <repo>` command.
//
// tags lists every tag in a repository plus per-tag metadata (digest, size).
// The full tag list is always fetched from the registry; per-tag HEAD lookups
// for metadata are capped by --limit unless --all is set. Tags past the
// metadata cap are still listed by name with "--" under the DIGEST/SIZE
// columns.
//
// --limit 0 means unlimited (Unix convention). --all is equivalent to
// --limit 0; the two flags together are redundant but not an error.
func NewCmdTags(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &tagsOptions{
		Profile: defaultProfileName,
		Limit:   defaultTagsLimit,
	}

	cmd := &cobra.Command{
		Use:   "tags <repo>",
		Short: "List tags in a container repository",
		Long: cmdutil.LongDesc(`
			List every tag in the given repository plus per-tag metadata
			(digest, size).

			The repository may be passed as a full reference
			("vccr.io/<project>/<name>") or a short reference ("<name>"). Short
			references are expanded using the active registry credentials. If
			a tag component is included in the argument (e.g. "app:v1"), it is
			ignored — tags always operates on the full repository.

			The full tag list is always fetched from the registry. Per-tag
			HEAD lookups for digest/size are capped by --limit to keep
			response time bounded on repositories with many tags. Tags past
			the cap are listed by name with "--" in the DIGEST and SIZE
			columns.

			Pass --all to run HEAD lookups for every tag regardless of
			--limit. --limit 0 is equivalent to --all.

			The "PUSHED" column is reserved for a future release and currently
			prints "--" for every row.
		`),
		Example: cmdutil.Examples(`
			# List tags in a short-form repository
			verda registry tags my-app

			# List tags in a fully qualified repository
			verda registry tags vccr.io/my-project/my-app

			# Raise the metadata cap
			verda registry tags my-app --limit 200

			# Metadata for every tag (slower on repositories with many tags)
			verda registry tags my-app --all

			# JSON output for scripts
			verda registry tags my-app -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTags(cmd, f, ioStreams, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared Verda credentials file")
	flags.IntVar(&opts.Limit, "limit", opts.Limit, "Cap on per-tag metadata lookups (0 = unlimited)")
	flags.BoolVar(&opts.All, "all", false, "Run metadata lookups for every tag (overrides --limit)")

	return cmd
}

// runTags is the RunE body, split out for testability.
//
// Concurrency note: per-tag HEAD lookups are issued SERIALLY for v1. Even at
// --limit 50 on a small-to-medium repository this is usually bounded at a
// handful of seconds. Parallelising with a bounded worker pool is a future
// optimisation — keeping the code path linear makes the spinner message
// honest and error surfaces obvious.
func runTags(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *tagsOptions, rawRepo string) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Same collapse as ls: any "no usable creds" shape surfaces as
		// registry_not_configured.
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}

	ref, err := Normalize(rawRepo, creds)
	if err != nil {
		return err
	}
	// If the caller typed a tag (e.g. "app:v1") surface a note to ErrOut so
	// agent-mode stdout remains clean. The tag is discarded — we only need
	// the repository path to list tags.
	if ref.Tag != "" && ref.Tag != "latest" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut,
			"Note: ignoring tag %q in %q; tags always lists every tag in the repository.\n",
			ref.Tag, rawRepo)
	}

	// tags does not yet expose a --retries flag; pass an empty
	// RetryConfig so the ggcr default transport is used.
	reg := buildClient(creds, RetryConfig{})

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	sp := startTagsSpinner(ctx, f, ref.FullRepository())
	fullRepo := ref.FullRepository()

	tags, tagsErr := reg.Tags(ctx, fullRepo)
	if tagsErr != nil {
		stopSpinner(sp)
		return translateErrorWithExpiry(tagsErr, creds)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
		fmt.Sprintf("API response: %d tag(s) for %s:", len(tags), fullRepo), tags)

	// Empty-tags case: render a friendly message (or empty structured payload)
	// without issuing any HEAD calls.
	if len(tags) == 0 {
		stopSpinner(sp)
		return writeEmptyTags(ioStreams, f, ref.String(), fullRepo)
	}

	// Registry v2 doesn't guarantee order; sort ascending lexicographically.
	// TODO: semver-aware sort is out of scope for v1.
	sort.Strings(tags)

	// Decide how many tags get HEAD lookups. --all or --limit 0 mean
	// "everything"; otherwise cap at opts.Limit.
	metadataCap := opts.Limit
	if opts.All || opts.Limit <= 0 {
		metadataCap = len(tags)
	}

	rows, headErr := fetchTagRows(ctx, reg, fullRepo, tags, metadataCap)
	if headErr != nil {
		stopSpinner(sp)
		return translateErrorWithExpiry(headErr, creds)
	}

	stopSpinner(sp)

	payload := tagsPayload{Repository: ref.String(), Tags: rows}
	if isStructuredFormat(f.OutputFormat()) {
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			return werr
		}
	}
	renderTagsHuman(ioStreams, payload, metadataCap)
	return nil
}

// startTagsSpinner starts the listing spinner (if the factory exposes a
// status impl). Returns nil when Status is unavailable — callers must handle
// a nil spinner.
func startTagsSpinner(ctx context.Context, f cmdutil.Factory, fullRepo string) interface{ Stop(string) } {
	status := f.Status()
	if status == nil {
		return nil
	}
	sp, _ := status.Spinner(ctx, fmt.Sprintf("Listing tags for %s...", fullRepo))
	return sp
}

// stopSpinner is a nil-safe shim so callers can skip a per-site nil guard.
func stopSpinner(sp interface{ Stop(string) }) {
	if sp != nil {
		sp.Stop("")
	}
}

// writeEmptyTags renders the no-tags empty-state, in structured or human mode.
func writeEmptyTags(ioStreams cmdutil.IOStreams, f cmdutil.Factory, refStr, fullRepo string) error {
	payload := tagsPayload{Repository: refStr, Tags: []tagRow{}}
	if isStructuredFormat(f.OutputFormat()) {
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			return werr
		}
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "No tags found in %s.\n", fullRepo)
	return nil
}

// fetchTagRows issues up to metadataCap serial HEAD lookups, building a tagRow
// per tag. Tags past the cap have Digest=="" and Size==0 (the "not looked up"
// state — the human renderer maps these back to "--").
func fetchTagRows(ctx context.Context, reg Registry, fullRepo string, tags []string, metadataCap int) ([]tagRow, error) {
	rows := make([]tagRow, 0, len(tags))
	for i, tag := range tags {
		row := tagRow{Tag: tag}
		if i < metadataCap {
			desc, err := reg.Head(ctx, fullRepo+":"+tag)
			if err != nil {
				return nil, err
			}
			if desc != nil {
				row.Digest = desc.Digest.String()
				row.Size = desc.Size
			}
		}
		// PushedAt stays nil — see tagRow doc comment + TODO.
		rows = append(rows, row)
	}
	return rows, nil
}

// renderTagsHuman prints the human-readable table to ioStreams.Out. Digests
// are truncated to "sha256:<12char>…" so rows fit on one line. Size is
// formatted as a binary-unit human-readable string (e.g. "4.2 GiB"). Rows
// past the metadata cap render "--" in the DIGEST and SIZE columns so users
// can see which rows were skipped.
func renderTagsHuman(ioStreams cmdutil.IOStreams, payload tagsPayload, metadataCap int) {
	if len(payload.Tags) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "No tags found in %s.\n", payload.Repository)
		return
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d tag(s) found in %s\n\n", len(payload.Tags), payload.Repository)
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-24s    %-22s    %-10s    %s\n", "TAG", "DIGEST", "SIZE", "PUSHED")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-24s    %-22s    %-10s    %s\n", "---", "------", "----", "------")

	for i, r := range payload.Tags {
		digestStr := "--"
		sizeStr := "--"
		if i < metadataCap {
			digestStr = truncateDigest(r.Digest)
			sizeStr = formatBytes(r.Size)
		}
		pushed := "--"
		if r.PushedAt != nil {
			pushed = r.PushedAt.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-24s    %-22s    %-10s    %s\n", r.Tag, digestStr, sizeStr, pushed)
	}
}

// truncateDigest renders a sha256 digest as "sha256:<first 12 hex chars>…".
// Non-sha256 or short inputs are returned unchanged (callers already handle
// the "--" sentinel for missing digests).
func truncateDigest(d string) string {
	if d == "" {
		return "--"
	}
	const prefix = "sha256:"
	if len(d) > len(prefix)+12 && d[:len(prefix)] == prefix {
		return d[:len(prefix)+12] + "…"
	}
	return d
}
