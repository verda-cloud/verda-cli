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

	"github.com/spf13/cobra"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// progressValue sentinels for the --progress flag.
const (
	progressNone = "none"
)

// pushOptions bundles flag state for `verda registry push`.
type pushOptions struct {
	Profile         string
	CredentialsFile string
	Repo            string // destination repo override (single-image only)
	Tag             string // destination tag override (single-image only)
	Source          string // auto|daemon|oci|tar
	Jobs            int    // layer-level parallelism (0 = ggcr default)
	ImageJobs       int    // image-level parallelism (v1: always 1, stored for future)
	Retries         int    // flows into the retrying http.RoundTripper
	Progress        string // auto|plain|json|none
	NoMount         bool   // disables cross-repo blob mount (v1: flag stored; not yet wired)
}

// pushResult is the per-image outcome collected by the sequential loop.
type pushResult struct {
	Ref string // the raw user-supplied source ref
	Dst string // the resolved destination ref (empty if resolution failed)
	Err error  // nil on success
}

// pushResultRow is the structured-output row per image. Matches the shape
// used by other verda commands: explicit status, explicit error code on
// failure so agent-mode consumers can dispatch on it.
type pushResultRow struct {
	Source      string `json:"source" yaml:"source"`
	Destination string `json:"destination,omitempty" yaml:"destination,omitempty"`
	Status      string `json:"status" yaml:"status"` // "pushed" | "failed"
	ErrorCode   string `json:"error_code,omitempty" yaml:"error_code,omitempty"`
	Error       string `json:"error,omitempty" yaml:"error,omitempty"`
}

// pushPayload is the top-level structured payload.
type pushPayload struct {
	Results []pushResultRow `json:"results" yaml:"results"`
}

// NewCmdPush creates the `verda registry push` command.
//
// push reads one or more local images (from the Docker daemon, an OCI
// layout directory, or a tarball) and uploads each to the configured
// Verda Container Registry. For v1 images are pushed sequentially; the
// --image-jobs flag is accepted and stored for the Task 22 bubbletea
// worker pool but has no effect yet.
func NewCmdPush(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &pushOptions{
		Profile:   defaultProfileName,
		Source:    string(SourceAuto),
		ImageJobs: 1,
		Retries:   5,
		Progress:  "auto",
	}

	cmd := &cobra.Command{
		Use:   "push <local-image> [<local-image>...]",
		Short: "Push local images to the container registry",
		Long: cmdutil.LongDesc(`
			Upload one or more local images to Verda Container Registry.

			Each positional argument is a local reference resolved through
			the --source pipeline:

			  - auto   (default) picks oci for directories, tar for .tar/.tar.gz
			           files, and daemon for everything else after probing the
			           Docker daemon.
			  - daemon reads the image from the local Docker daemon.
			  - oci    reads an OCI image layout from a directory.
			  - tar    reads a Docker/OCI tarball from a file.

			The destination repository defaults to the local reference's
			repository name under the active credentials' project; use
			--repo / --tag to override (single-image only).

			For v1 images are pushed sequentially; parallelism across
			images will arrive with the interactive progress view.
		`),
		Example: cmdutil.Examples(`
			# Push a single image from the Docker daemon
			verda registry push my-app:v1

			# Push multiple images sequentially
			verda registry push my-app:v1 worker:v1 edge:v1

			# Push with a different destination repo and tag
			verda registry push my-app:latest --repo team/api --tag prod

			# Push from an OCI layout directory
			verda registry push --source oci ./build/image

			# Push from a tarball
			verda registry push --source tar ./out/image.tar

			# JSON output for scripts
			verda registry push my-app:v1 -o json
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to credentials file")
	flags.StringVar(&opts.Repo, "repo", "", "Override destination repository name (single-image only)")
	flags.StringVar(&opts.Tag, "tag", "", "Override destination tag (single-image only)")
	flags.StringVar(&opts.Source, "source", opts.Source, "Image source: auto|daemon|oci|tar")
	flags.IntVar(&opts.Jobs, "jobs", 0, "Layer-level parallelism (0 = ggcr default)")
	flags.IntVar(&opts.ImageJobs, "image-jobs", opts.ImageJobs, "Image-level parallelism (v1: always 1)")
	flags.IntVar(&opts.Retries, "retries", opts.Retries, "Maximum attempts for idempotent HTTP operations on transient errors")
	flags.StringVar(&opts.Progress, "progress", opts.Progress, "Progress output: auto|plain|json|none")
	flags.BoolVar(&opts.NoMount, "no-mount", false, "Disable cross-repo blob mount (v1: flag is a no-op)")

	return cmd
}

// runPush is the RunE body, split out for testability.
func runPush(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *pushOptions, args []string) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Any "no usable creds" shape collapses to registry_not_configured,
		// matching ls/tags. The loader is never called in that case.
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}

	if len(args) == 0 {
		// Interactive multi-select picker is Task 23/24. For now surface a
		// structured error pointing the user at the flag-driven usage.
		return &cmdutil.AgentError{
			Code: kindRegistryInvalidReference,
			Message: "interactive push picker is not yet wired; " +
				"pass at least one image reference as a positional argument " +
				"(e.g. `verda registry push my-app:v1`)",
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	if len(args) > 1 && (opts.Repo != "" || opts.Tag != "") {
		return cmdutil.UsageErrorf(cmd,
			"--repo and --tag cannot be combined with multiple images (ambiguous destination)")
	}

	src := ImageSource(opts.Source)

	// Route push's --retries into the retrying transport. Retries only
	// fire for idempotent operations (GET/HEAD/PUT/DELETE/PATCH); blob
	// upload POSTs are excluded by design.
	reg := buildClient(creds, RetryConfig{MaxAttempts: opts.Retries})

	ping := func(ctx context.Context) error {
		lister, err := daemonListerBuilder()
		if err != nil {
			return err
		}
		return lister.Ping(ctx)
	}
	loader := sourceLoaderBuilder(ping)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	// --no-mount is accepted but not yet wired: ggcr's remote.Write always
	// attempts cross-repo blob mount. Warn once on ErrOut so users get
	// honest feedback — wiring lands alongside the retry transport.
	if opts.NoMount {
		_, _ = fmt.Fprintln(ioStreams.ErrOut,
			"note: --no-mount is not yet wired; cross-repo blob mount remains enabled for v1")
	}

	progressEnabled := !isStructuredFormat(f.OutputFormat()) && opts.Progress != progressNone

	results := make([]pushResult, 0, len(args))
	for _, rawLocal := range args {
		r := pushOneImage(ctx, reg, loader, creds, src, rawLocal, opts, ioStreams, progressEnabled)
		results = append(results, r)
	}

	if isStructuredFormat(f.OutputFormat()) {
		payload := buildPushPayload(results)
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			if werr != nil {
				return werr
			}
			return firstError(results)
		}
	}

	for _, r := range results {
		if r.Err != nil {
			dst := r.Dst
			if dst == "" {
				dst = "<unresolved>"
			}
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "FAILED %s -> %s: %v\n", r.Ref, dst, r.Err)
			continue
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "pushed %s -> %s\n", r.Ref, r.Dst)
	}

	return firstError(results)
}

// pushOneImage loads and pushes a single image. It handles its own progress
// goroutine so the channel is always drained to completion (ggcr closes it
// on Write return).
func pushOneImage(
	ctx context.Context,
	reg Registry,
	loader SourceLoader,
	creds *options.RegistryCredentials,
	src ImageSource,
	rawLocal string,
	opts *pushOptions,
	ioStreams cmdutil.IOStreams,
	progressEnabled bool,
) pushResult {
	img, err := loader.Load(ctx, src, rawLocal)
	if err != nil {
		return pushResult{Ref: rawLocal, Err: err}
	}
	dst, err := resolveDestination(rawLocal, creds, opts.Repo, opts.Tag)
	if err != nil {
		return pushResult{Ref: rawLocal, Err: err}
	}

	// Size the buffer generously so ggcr's layer-producer goroutines don't
	// block when the drain goroutine is momentarily slow writing to ErrOut.
	progressCh := make(chan v1.Update, 16)
	drainDone := make(chan struct{})
	go func() {
		drainProgress(progressCh, ioStreams.ErrOut, dst, progressEnabled)
		close(drainDone)
	}()

	wo := WriteOptions{Jobs: opts.Jobs, Progress: progressCh}
	writeErr := reg.Write(ctx, dst, img, wo)
	// ggcr closes progressCh on Write return; drain goroutine exits, then
	// we continue. Waiting here guarantees no goroutine leak and that any
	// final progress line has flushed to ErrOut before the summary row.
	<-drainDone

	return pushResult{
		Ref: rawLocal,
		Dst: dst,
		Err: translateErrorWithExpiry(writeErr, creds),
	}
}

// drainProgress consumes updates from ch until it closes. When enabled, a
// final "pushed ..." line is written to w summarizing the total bytes
// transferred for dst. Per-update lines are NOT emitted (Task 22/20 wires
// the fancy view) — a single completion line keeps the human flow compact
// while still giving tests a visible signal.
//
// This function never writes when enabled==false so structured output
// (JSON/YAML) stays clean.
func drainProgress(ch <-chan v1.Update, w io.Writer, dst string, enabled bool) {
	var last v1.Update
	var gotAny bool
	for u := range ch {
		last = u
		gotAny = true
	}
	if !enabled || !gotAny {
		return
	}
	// If the final update reported an error, don't print a success line —
	// the surrounding error-handling path will emit the FAILED row.
	if last.Error != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "pushed layer data for %s (%d/%d bytes)\n", dst, last.Complete, last.Total)
}

// resolveDestination computes the final destination reference from a local
// (source) reference plus optional --repo / --tag overrides and the active
// registry credentials.
//
// Parsing is intentionally manual rather than routed through our own
// Normalize(): Normalize prepends creds.ProjectID, which is correct for
// talking to VCR but would turn a local "my-app:v1" into
// "<host>/<project>/my-app:v1" on the SOURCE side — we only want that
// prefix on the destination side.
//
// Splitting rules (matches Docker's own ref grammar for the relevant subset):
//
//   - digest component ("@sha256:...") is stripped: push destinations carry
//     a tag, not a digest.
//   - tag is the substring after the LAST ":" provided that ":" is not part
//     of an authority ("host:port") prefix.
//   - repository is everything left after trimming an optional leading
//     "<host>[:<port>]/" segment. Multi-segment paths like "team/app" are
//     preserved as-is.
//
// Returns an AgentError when the computed repo or tag is empty (which
// would otherwise produce a malformed destination).
func resolveDestination(rawLocal string, creds *options.RegistryCredentials, repoFlag, tagFlag string) (string, error) {
	if creds == nil || creds.Endpoint == "" || creds.ProjectID == "" {
		return "", &cmdutil.AgentError{
			Code:     kindRegistryNotConfigured,
			Message:  "Registry is not configured. Run `verda registry configure` first.",
			ExitCode: cmdutil.ExitAuth,
		}
	}

	repo, tag, err := splitLocalRef(rawLocal)
	if err != nil {
		return "", err
	}
	if repoFlag != "" {
		repo = repoFlag
	}
	if tagFlag != "" {
		tag = tagFlag
	}
	if repo == "" {
		return "", &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("cannot derive destination repository from %q; pass --repo to override", rawLocal),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	if tag == "" {
		tag = "latest"
	}

	return creds.Endpoint + "/" + creds.ProjectID + "/" + repo + ":" + tag, nil
}

// splitLocalRef extracts (repository, tag) from a local image reference
// without consulting any default registry. The host-trimming heuristic
// mirrors the one in refname.go's isShortRef: the first "/"-delimited
// segment is treated as a host only if it contains "." or ":" or equals
// "localhost".
func splitLocalRef(raw string) (repo, tag string, err error) {
	if raw == "" {
		return "", "", &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  "empty image reference",
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	// Strip an @digest suffix if present — the destination side always uses a tag.
	if at := strings.IndexByte(raw, '@'); at >= 0 {
		raw = raw[:at]
	}

	// Split off a leading host[:port] segment, matching isShortRef.
	path := raw
	if slash := strings.IndexByte(raw, '/'); slash > 0 {
		first := raw[:slash]
		if first == localhostHost || strings.ContainsAny(first, ".:") {
			path = raw[slash+1:]
		}
	}

	// Now split tag (anything after the LAST ":" in the remaining path).
	// A colon inside a path segment is invalid, so the last-colon rule is safe.
	if colon := strings.LastIndexByte(path, ':'); colon >= 0 {
		repo = path[:colon]
		tag = path[colon+1:]
	} else {
		repo = path
		tag = ""
	}

	if repo == "" {
		return "", "", &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("invalid image reference %q: missing repository", raw),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	return repo, tag, nil
}

// buildPushPayload converts the internal result slice into the structured
// payload shape.
func buildPushPayload(results []pushResult) pushPayload {
	rows := make([]pushResultRow, 0, len(results))
	for _, r := range results {
		row := pushResultRow{
			Source:      r.Ref,
			Destination: r.Dst,
			Status:      "pushed",
		}
		if r.Err != nil {
			row.Status = "failed"
			row.Error = r.Err.Error()
			var ae *cmdutil.AgentError
			if errors.As(r.Err, &ae) {
				row.ErrorCode = ae.Code
			}
		}
		rows = append(rows, row)
	}
	return pushPayload{Results: rows}
}

// firstError returns the first non-nil error in results, or nil. The
// sequential loop always attempts every image; we surface exit-non-zero
// via the first failure so callers get a deterministic signal.
func firstError(results []pushResult) error {
	for _, r := range results {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}
