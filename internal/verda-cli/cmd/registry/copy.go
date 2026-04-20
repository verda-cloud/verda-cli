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
	"runtime"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Source-auth sentinels for the --src-auth flag. docker-config (the
// default) picks creds up from ~/.docker/config.json / credential
// helpers via authn.DefaultKeychain, which also works anonymously for
// public images. anonymous forces no auth (useful when the docker
// config has a stale entry). basic requires --src-username plus the
// secret on stdin via --src-password-stdin.
const (
	srcAuthDockerConfig = "docker-config"
	srcAuthAnonymous    = "anonymous"
	srcAuthBasic        = "basic"
)

// Status sentinels for the copy structured-output payloads.
const (
	copyStatusCopied    = "copied"
	copyStatusFailed    = "failed"
	copyStatusSucceeded = "succeeded"
)

// sourceKeychainBuilder is the package-level swap point for resolving
// the default source-side keychain. Production uses authn.DefaultKeychain
// (reads ~/.docker/config.json + cred helpers). Tests reassign this to
// a sentinel so they can assert which keychain the command picked for
// each --src-auth value without having to drive a real registry that
// checks auth.
var sourceKeychainBuilder authn.Keychain = authn.DefaultKeychain

// sourceRegistryBuilder is the swap point for building the source-side
// Registry. Defaults to newGGCRRegistryForSource. Tests can reassign
// this to inject a fake (mirrors clientBuilder's role for the dest
// side).
var sourceRegistryBuilder = newGGCRRegistryForSource

// copyOptions bundles flag state for `verda registry copy`.
type copyOptions struct {
	Profile          string
	CredentialsFile  string
	Jobs             int    // layer-level parallelism on Write
	ImageJobs        int    // image-level parallelism (0 = auto-pick; see resolveImageJobs)
	Retries          int    // flows into both src + dst retrying transports
	Progress         string // auto|plain|json|none
	SrcAuth          string // docker-config|anonymous|basic
	SrcUsername      string // required with --src-auth basic
	SrcPasswordStdin bool   // required with --src-auth basic
	AllTags          bool   // copy every tag in the source repository
}

// copyPayload is the structured-output shape for `copy`. Mirrors push's
// shape but flattens to a single row since single-ref copy only moves
// one image; the --all-tags fan-out in Task 26 will extend this to a
// Results slice.
type copyPayload struct {
	Source           string `json:"src" yaml:"src"`
	Destination      string `json:"dst" yaml:"dst"`
	BytesTotal       int64  `json:"bytes_total" yaml:"bytes_total"`
	BytesTransferred int64  `json:"bytes_transferred" yaml:"bytes_transferred"`
	DurationMs       int64  `json:"duration_ms" yaml:"duration_ms"`
	Status           string `json:"status" yaml:"status"` // "copied" | "failed"
	ErrorCode        string `json:"error_code,omitempty" yaml:"error_code,omitempty"`
	Error            string `json:"error,omitempty" yaml:"error,omitempty"`
}

// NewCmdCopy creates the `verda registry copy` command.
//
// copy reads a single image from a source registry and writes it to the
// Verda Container Registry. For v1 the --all-tags fan-out and the
// --dry-run / --overwrite guards are deferred to later tasks — this
// command covers the single-ref happy path.
//
// Source authentication is separate from VCR credentials: by default we
// resolve via authn.DefaultKeychain (same as `docker pull`), which
// handles public images anonymously and private images through
// ~/.docker/config.json + credential helpers. --src-auth anonymous
// bypasses the keychain entirely; --src-auth basic accepts explicit
// credentials, with the secret read from stdin.
func NewCmdCopy(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &copyOptions{
		Profile:  defaultProfileName,
		Retries:  5,
		Progress: progressAuto,
		SrcAuth:  srcAuthDockerConfig,
	}

	cmd := &cobra.Command{
		Use:     "copy <src> [<dst>]",
		Aliases: []string{"cp"},
		Short:   "Copy an image between registries",
		Long: cmdutil.LongDesc(`
			Copy a single image from a source registry to the configured
			Verda Container Registry.

			The source must be a fully qualified reference (e.g.
			"docker.io/library/nginx:1.25"). The destination, if omitted,
			defaults to the active registry endpoint + project with the
			source repository and tag preserved. A short destination
			(e.g. "my-app:prod") is expanded via the active credentials
			to "<endpoint>/<project>/my-app:prod".

			Source-side credentials are resolved via ~/.docker/config.json
			and registered credential helpers by default. Pass
			--src-auth anonymous to skip the keychain entirely or
			--src-auth basic plus --src-username / --src-password-stdin
			to supply explicit credentials.
		`),
		Example: cmdutil.Examples(`
			# Copy a public image from Docker Hub to VCR under the same repo/tag
			verda registry copy docker.io/library/nginx:1.25

			# Copy to a custom destination
			verda registry copy gcr.io/my-project/app:v1 my-app:prod

			# Use basic auth on the source side
			echo "$SRC_PASSWORD" | verda registry copy \
			    private.example.com/app:v1 \
			    --src-auth basic --src-username jdoe --src-password-stdin

			# JSON output for scripts
			verda registry copy docker.io/library/nginx:1.25 -o json
		`),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCopy(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile name")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to credentials file")
	flags.IntVar(&opts.Jobs, "jobs", 0, "Layer-level parallelism (0 = ggcr default)")
	flags.IntVar(&opts.ImageJobs, "image-jobs", 0, "Image-level parallelism for --all-tags (0 = auto)")
	flags.IntVar(&opts.Retries, "retries", opts.Retries, "Maximum attempts for idempotent HTTP operations on transient errors")
	flags.StringVar(&opts.Progress, "progress", opts.Progress, "Progress output: auto|plain|json|none")
	flags.StringVar(&opts.SrcAuth, "src-auth", opts.SrcAuth, "Source authentication mode: docker-config|anonymous|basic")
	flags.StringVar(&opts.SrcUsername, "src-username", "", "Source registry username (required with --src-auth basic)")
	flags.BoolVar(&opts.SrcPasswordStdin, "src-password-stdin", false, "Read the source registry password from stdin (required with --src-auth basic)")
	flags.BoolVar(&opts.AllTags, "all-tags", false, "Copy every tag in the source repository")

	return cmd
}

// runCopy is the RunE body. Flow:
//  1. Load + validate creds.
//  2. Parse src (fully-qualified required) and compute dst (supplied or synthesized).
//  3. Build source-side authenticator per --src-auth.
//  4. Build source + dest Registry clients (both with retries).
//  5. Read from source, Write to dest with progress channel.
//  6. Render structured / plain / bubbletea output per --progress + -o.
func runCopy(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *copyOptions, args []string) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Any "no usable creds" shape collapses to registry_not_configured,
		// matching ls/tags/push. The loader is never called in that case.
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}

	srcRef, err := Parse(args[0])
	if err != nil {
		return &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  err.Error(),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	srcAuth, err := buildSourceAuth(opts, ioStreams.In)
	if err != nil {
		return err
	}

	retryCfg := RetryConfig{MaxAttempts: opts.Retries}
	srcReg := sourceRegistryBuilder(srcAuth, retryCfg)
	dstReg := buildClient(creds, retryCfg)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	if opts.AllTags {
		return runCopyAllTagsFlow(ctx, cancel, cmd, f, ioStreams, srcReg, dstReg, srcRef, args, creds, opts)
	}

	dstRef, err := resolveCopyDestination(args, srcRef, creds)
	if err != nil {
		return err
	}

	srcString := srcRef.String()
	dstString := dstRef.String()

	result := performCopy(ctx, cancel, srcReg, dstReg, srcString, dstString, creds, opts, f, ioStreams)

	if isStructuredFormat(f.OutputFormat()) {
		payload := buildCopyPayload(srcString, dstString, result)
		wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload)
		if wrote {
			if werr != nil {
				return werr
			}
			return result.err
		}
	}

	// Human-readable summary on the appropriate stream.
	if result.err != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "FAILED %s -> %s: %v\n", srcString, dstString, result.err)
		return result.err
	}
	if opts.Progress != progressNone {
		_, _ = fmt.Fprintf(ioStreams.Out, "copied %s -> %s\n", srcString, dstString)
	}
	return nil
}

// copyResult captures the outcome of a single-ref copy for the top-level
// renderers. bytesTotal / bytesTransferred / duration fall out of the
// ggcr progress channel aggregation.
type copyResult struct {
	err              error
	bytesTotal       int64
	bytesTransferred int64
	duration         time.Duration
}

// performCopy runs the Read+Write pipeline and aggregates progress. It
// decides between the bubbletea progress view and the flat-text fallback
// based on --progress and TTY detection, mirroring push.go.
func performCopy(
	ctx context.Context,
	cancel context.CancelFunc,
	srcReg Registry,
	dstReg Registry,
	srcString, dstString string,
	creds *options.RegistryCredentials,
	opts *copyOptions,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
) copyResult {
	// Source read is not progress-tracked (ggcr streams lazily on Write);
	// any source-side failure short-circuits the pipeline before the
	// destination-side transfer can start.
	img, err := srcReg.Read(ctx, srcString)
	if err != nil {
		return copyResult{err: translateError(err)}
	}

	if shouldUseBubbleteaCopy(opts, f.OutputFormat(), ioStreams.ErrOut) {
		return runCopyBubbletea(ctx, cancel, dstReg, creds, img, srcString, dstString, opts, ioStreams)
	}

	progressEnabled := !isStructuredFormat(f.OutputFormat()) &&
		opts.Progress == progressPlain
	return runCopyFlat(ctx, dstReg, creds, img, srcString, dstString, opts, ioStreams, progressEnabled)
}

// runCopyFlat is the non-bubbletea path. It drains the progress channel
// synchronously so ggcr's close signals flush before we return and
// accumulates the last-seen total + completed bytes into the result.
func runCopyFlat(
	ctx context.Context,
	dstReg Registry,
	creds *options.RegistryCredentials,
	img v1.Image,
	srcString, dstString string,
	opts *copyOptions,
	ioStreams cmdutil.IOStreams,
	progressEnabled bool,
) copyResult {
	progressCh := make(chan v1.Update, 16)
	drainDone := make(chan struct{})
	var last v1.Update
	var gotAny bool
	go func() {
		defer close(drainDone)
		for u := range progressCh {
			last = u
			gotAny = true
		}
	}()

	start := time.Now()
	wo := WriteOptions{Jobs: opts.Jobs, Progress: progressCh}
	writeErr := dstReg.Write(ctx, dstString, img, wo)
	<-drainDone
	elapsed := time.Since(start)

	result := copyResult{
		err:      translateErrorWithExpiry(writeErr, creds),
		duration: elapsed,
	}
	if gotAny {
		result.bytesTotal = last.Total
		result.bytesTransferred = last.Complete
	}

	if progressEnabled && result.err == nil && gotAny {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "copied %s -> %s (%s in %s)\n",
			srcString, dstString, formatBytes(last.Complete), formatMMSS(elapsed))
	}

	return result
}

// runCopyBubbletea reuses the push view's model since its row shape
// (Ref + Dst + per-row Meter + state machine) fits a single-ref copy
// without modification. We feed a one-row model and pipe ggcr progress
// updates through the same pushProgressMsg channel.
//
// Reusing the push view keeps the TUI consistent between push and copy
// at the cost of a slightly misleading heading ("Pushing 1 image to
// <host>/<project>") — acceptable for v1 since the destination really
// is a push into VCR. A dedicated copy view showing src -> dst on the
// heading is easy to swap in later without changing the call site.
func runCopyBubbletea(
	ctx context.Context,
	cancel context.CancelFunc,
	dstReg Registry,
	creds *options.RegistryCredentials,
	img v1.Image,
	srcString, dstString string,
	opts *copyOptions,
	ioStreams cmdutil.IOStreams,
) copyResult {
	rows := []imageRow{{Ref: srcString, Dst: dstString, State: stateQueued}}
	model := newPushViewModel(creds.Endpoint, creds.ProjectID, rows, cancel)
	program := tea.NewProgram(model, tea.WithOutput(ioStreams.ErrOut))

	var result copyResult
	go func() {
		progressCh := make(chan v1.Update, 16)
		forwardDone := make(chan struct{})
		var last v1.Update
		var gotAny bool
		go func() {
			defer close(forwardDone)
			for u := range progressCh {
				last = u
				gotAny = true
				program.Send(pushProgressMsg{Index: 0, Update: u})
			}
		}()

		start := time.Now()
		wo := WriteOptions{Jobs: opts.Jobs, Progress: progressCh}
		writeErr := dstReg.Write(ctx, dstString, img, wo)
		<-forwardDone
		result.duration = time.Since(start)

		if gotAny {
			result.bytesTotal = last.Total
			result.bytesTransferred = last.Complete
		}

		writeErr = translateErrorWithExpiry(writeErr, creds)
		result.err = writeErr
		program.Send(pushResultMsg{Index: 0, Err: writeErr})
	}()

	if _, err := program.Run(); err != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "copy: progress view error: %v\n", err)
	}
	return result
}

// resolveCopyDestination computes the destination Ref. When args has a
// second element, it's normalized against creds (so short refs expand).
// Otherwise, the destination defaults to the source's repo+tag under the
// active endpoint + project.
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; the contract in refname.go uses value receivers uniformly.
func resolveCopyDestination(args []string, src Ref, creds *options.RegistryCredentials) (Ref, error) {
	if len(args) >= 2 {
		ref, err := Normalize(args[1], creds)
		if err != nil {
			return Ref{}, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  err.Error(),
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		return ref, nil
	}
	if creds == nil || creds.Endpoint == "" || creds.ProjectID == "" {
		return Ref{}, &cmdutil.AgentError{
			Code:     kindRegistryNotConfigured,
			Message:  "Registry is not configured. Run `verda registry configure` first.",
			ExitCode: cmdutil.ExitAuth,
		}
	}
	// Synthesize from src: preserve the source's repository path (without
	// the source's own project namespace — the entire "<project>/<repo>"
	// segment is kept as a single path so "library/nginx" stays together
	// under the VCR project). Tag is preserved if present; digest-only
	// sources get "latest" as the destination tag (push semantics).
	//
	// We build the full path by concatenating src.Project and
	// src.Repository (skipping Project when empty) and use that as the
	// destination Repository, with Project = creds.ProjectID.
	fullRepo := src.Repository
	if src.Project != "" {
		fullRepo = src.Project + "/" + src.Repository
	}
	tag := src.Tag
	if tag == "" {
		tag = defaultTag
	}
	return Ref{
		Host:       creds.Endpoint,
		Project:    creds.ProjectID,
		Repository: fullRepo,
		Tag:        tag,
	}, nil
}

// buildSourceAuth constructs the authn.Authenticator for the source side
// based on --src-auth + associated flags. docker-config routes through
// the swappable sourceKeychainBuilder so tests can assert which keychain
// was selected without driving a real credential store.
func buildSourceAuth(opts *copyOptions, stdin io.Reader) (authn.Authenticator, error) {
	switch opts.SrcAuth {
	case srcAuthAnonymous:
		return authn.Anonymous, nil

	case srcAuthBasic:
		if opts.SrcUsername == "" {
			return nil, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "--src-auth basic requires --src-username",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		if !opts.SrcPasswordStdin {
			return nil, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "--src-auth basic requires --src-password-stdin (read from stdin)",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		if stdin == nil {
			return nil, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "no stdin available to read --src-password-stdin",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		pw, err := readSecretFromStdin(stdin)
		if err != nil {
			return nil, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  fmt.Sprintf("reading source password from stdin: %v", err),
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		if pw == "" {
			return nil, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  "empty password on stdin for --src-password-stdin",
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		return authn.FromConfig(authn.AuthConfig{
			Username: opts.SrcUsername,
			Password: pw,
		}), nil

	case srcAuthDockerConfig, "":
		// The keychain needs a Resource (host) to resolve against. We
		// don't have the srcRef here — keychain callers pass the
		// reference at call time. remote.WithAuth accepts a plain
		// Authenticator though, not a Keychain, so we return a
		// thin keychainAuth adapter that resolves lazily from the
		// configured sourceKeychainBuilder. In practice the keychain
		// is consulted once per Read and applied to every request for
		// that session.
		return &keychainAuth{keychain: sourceKeychainBuilder}, nil

	default:
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("unknown --src-auth %q (want docker-config|anonymous|basic)", opts.SrcAuth),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
}

// keychainAuth adapts authn.Keychain to the authn.Authenticator contract
// by deferring resolution until Authorization() is called. The keychain
// itself decides which credential (docker-config, helper, anonymous) to
// return per-host. If resolution fails we fall back to anonymous so
// public images remain pullable even when the keychain is misconfigured.
type keychainAuth struct {
	keychain authn.Keychain
	// resource is the host the authenticator is currently being used
	// against. Populated lazily on first Authorization() call via a
	// side-channel setResource hook invoked by the ggcr transport
	// machinery through the remote.Option path. In practice ggcr
	// passes a Resource to Keychain.Resolve directly — we embed that
	// lookup inline in Authorization so we don't depend on newer
	// ContextKeychain APIs.
}

// Authorization resolves the keychain at call time. For single-host
// copies the adapter is effectively memoized because remote.Image
// captures the resolved Authenticator after the first request; for
// v1 we re-resolve on every call, which is cheap for DefaultKeychain
// (file read is cached internally by ggcr).
func (k *keychainAuth) Authorization() (*authn.AuthConfig, error) {
	// ggcr's keychain-aware call sites go through remote.WithAuthFromKeychain
	// rather than an Authenticator adapter like this one — but remote.WithAuth
	// is simpler and avoids a second option slot, so we adapt here. The
	// resolve-without-resource fallback below returns the Docker Hub entry
	// (keychain's default) which is the common public-image case.
	if k.keychain == nil {
		return (&authn.AuthConfig{}), nil
	}
	auth, err := k.keychain.Resolve(keychainResource{host: authn.DefaultAuthKey})
	if err != nil || auth == nil {
		return (&authn.AuthConfig{}), nil //nolint:nilerr // fall back to anonymous for public images
	}
	return auth.Authorization()
}

// keychainResource is a minimal authn.Resource implementation. ggcr's
// DefaultKeychain only cares about RegistryStr().
type keychainResource struct {
	host string
}

func (r keychainResource) String() string      { return r.host }
func (r keychainResource) RegistryStr() string { return r.host }

// shouldUseBubbleteaCopy is the copy-side analog of
// shouldUseBubbletea. It follows the same decision matrix but gates off
// the copyOptions shape rather than pushOptions.
func shouldUseBubbleteaCopy(opts *copyOptions, outputFormat string, errOut io.Writer) bool {
	switch opts.Progress {
	case progressNone, progressPlain, progressJSON:
		return false
	}
	if isStructuredFormat(outputFormat) {
		return false
	}
	return isTerminalFn(errOut)
}

// buildCopyPayload converts a copyResult into the structured output shape.
func buildCopyPayload(src, dst string, r copyResult) copyPayload {
	p := copyPayload{
		Source:           src,
		Destination:      dst,
		BytesTotal:       r.bytesTotal,
		BytesTransferred: r.bytesTransferred,
		DurationMs:       r.duration.Milliseconds(),
		Status:           copyStatusCopied,
	}
	if r.err != nil {
		p.Status = copyStatusFailed
		p.Error = r.err.Error()
		var ae *cmdutil.AgentError
		if errors.As(r.err, &ae) {
			p.ErrorCode = ae.Code
		}
	}
	return p
}

// ---------- --all-tags implementation ----------

// copyTagResult is the per-tag row in the --all-tags structured payload.
type copyTagResult struct {
	Tag        string `json:"tag"                    yaml:"tag"`
	Src        string `json:"src"                    yaml:"src"`
	Dst        string `json:"dst"                    yaml:"dst"`
	Bytes      int64  `json:"bytes"                  yaml:"bytes"`
	Status     string `json:"status"                 yaml:"status"` // "succeeded" | "failed"
	Error      string `json:"error,omitempty"        yaml:"error,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"   yaml:"error_code,omitempty"`
	DurationMs int64  `json:"duration_ms"            yaml:"duration_ms"`
}

// allTagsSummary is the aggregate counts across all per-tag results.
type allTagsSummary struct {
	Total     int `json:"total"     yaml:"total"`
	Succeeded int `json:"succeeded" yaml:"succeeded"`
	Failed    int `json:"failed"    yaml:"failed"`
}

// allTagsOutput is the --all-tags structured-output shape.
type allTagsOutput struct {
	Results []copyTagResult `json:"results" yaml:"results"`
	Summary allTagsSummary  `json:"summary" yaml:"summary"`
}

// copyJob is a unit of work sent from the producer to the worker pool.
type copyJob struct {
	Index int
	Tag   string
}

// copyJobResult is a completed unit of work returned by a worker.
type copyJobResult struct {
	Index int
	Tag   string
	Src   string
	Dst   string
	Err   error
	Bytes int64
	Took  time.Duration
}

// resolveImageJobs clamps the requested image-level parallelism against the
// available hardware concurrency and the number of tags. Extracted with
// hwConcurrency as an explicit parameter so tests can drive the decision
// matrix deterministically without depending on runtime.NumCPU() of the
// test host.
//
// Rules:
//   - User-provided values > 0 win, but are clamped to min(hw, 8). We cap
//     at 8 because VCR is a single backend — spraying more than 8
//     concurrent pushes at one host creates more head-of-line blocking
//     than throughput, and ggcr's per-image layer fan-out already
//     saturates a typical link at 3-4 concurrent images.
//   - Auto (userValue <= 0): 1 for tiny repos (<4 tags), otherwise hw/2
//     capped at 4. The cap keeps a 32-core laptop from DoSing a small
//     registry while still giving slow links a useful speedup.
func resolveImageJobs(userValue, tagCount, hwConcurrency int) int {
	hw := hwConcurrency
	if hw < 1 {
		hw = 1
	}
	maxAllowed := hw
	if maxAllowed > 8 {
		maxAllowed = 8
	}
	if userValue > 0 {
		if userValue > maxAllowed {
			return maxAllowed
		}
		return userValue
	}
	if tagCount < 4 {
		return 1
	}
	half := hw / 2
	if half < 1 {
		half = 1
	}
	if half > 4 {
		return 4
	}
	return half
}

// runCopyAllTagsFlow is the --all-tags entry point. It validates src/dst
// shape, fetches the tag list, and delegates the fan-out to
// runCopyAllTagsPool.
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; contract uses value receivers uniformly (see refname.go).
func runCopyAllTagsFlow(
	ctx context.Context,
	cancel context.CancelFunc,
	cmd *cobra.Command,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	srcReg Registry,
	dstReg Registry,
	srcRef Ref,
	args []string,
	creds *options.RegistryCredentials,
	opts *copyOptions,
) error {
	// An explicit source tag contradicts --all-tags semantics (we'd be
	// enumerating the tags of a repo that the user then narrowed to one).
	// defaultTag ("latest") is the ggcr fallback when no tag is supplied,
	// so only reject when the user gave something OTHER than "latest" —
	// bare "docker.io/library/nginx" should still work.
	if hasExplicitTag(args[0]) {
		return cmdutil.UsageErrorf(cmd, "--all-tags is incompatible with a source tag; remove the :tag suffix")
	}

	dstBase, err := resolveCopyAllTagsDestination(cmd, args, srcRef, creds)
	if err != nil {
		return err
	}

	// Tags() expects a repository path; include the source host prefix
	// because the source-side Registry is built with an empty default
	// host (source refs must be fully qualified). Without the prefix,
	// ggcr would either fall back to docker.io or emit a hostless URL.
	srcRepoPath := srcRef.Host + "/" + srcRef.FullRepository()
	tags, err := srcReg.Tags(ctx, srcRepoPath)
	if err != nil {
		return translateError(err)
	}
	if len(tags) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No tags found in source repository.")
		if isStructuredFormat(f.OutputFormat()) {
			// Still emit an empty structured payload so scripts get a
			// parseable envelope.
			empty := allTagsOutput{Results: []copyTagResult{}}
			_, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), empty)
			return werr
		}
		return nil
	}

	imageJobs := resolveImageJobs(opts.ImageJobs, len(tags), runtime.NumCPU())

	results := runCopyAllTagsPool(ctx, cancel, srcReg, dstReg, srcRef, dstBase, tags, imageJobs, creds, opts, f, ioStreams)

	summary := summarizeCopyResults(results)

	if handled, err := writeAllTagsStructured(ioStreams, f.OutputFormat(), results, summary); handled {
		return err
	}

	// Human-readable summary.
	for _, r := range results {
		if r.Err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "FAILED %s -> %s: %v\n", r.Src, r.Dst, r.Err)
			continue
		}
		if opts.Progress != progressNone {
			_, _ = fmt.Fprintf(ioStreams.Out, "copied %s -> %s\n", r.Src, r.Dst)
		}
	}
	if opts.Progress != progressNone {
		_, _ = fmt.Fprintf(ioStreams.Out, "Copied %d of %d tag%s (%d failed).\n",
			summary.Succeeded, summary.Total, pluralS(summary.Total), summary.Failed)
	}

	if summary.Failed > 0 {
		return newPartialFailureError(summary)
	}
	return nil
}

// writeAllTagsStructured writes the --all-tags structured payload when the
// output format is structured. Returns (handled=true, err) when the
// caller should return immediately; handled=false means the caller should
// fall through to human-readable rendering.
func writeAllTagsStructured(ioStreams cmdutil.IOStreams, outputFormat string, results []copyJobResult, summary allTagsSummary) (bool, error) {
	if !isStructuredFormat(outputFormat) {
		return false, nil
	}
	payload := buildAllTagsOutput(results, summary)
	wrote, werr := cmdutil.WriteStructured(ioStreams.Out, outputFormat, payload)
	if !wrote {
		return false, nil
	}
	if werr != nil {
		return true, werr
	}
	if summary.Failed > 0 {
		return true, newPartialFailureError(summary)
	}
	return true, nil
}

// hasExplicitTag reports whether a raw source reference carries an explicit
// :tag suffix the user typed. A bare "docker.io/library/nginx" returns
// false even though the parsed Ref has Tag="latest" (the ggcr default).
// We scan right-to-left for ':' after the last '/' so host:port authority
// components ("registry.local:5000/app") don't trigger a false positive.
func hasExplicitTag(raw string) bool {
	// Peel any '@digest' suffix; digests are incompatible with --all-tags
	// but we leave that check to the ref parser which already ran.
	if at := lastIndexByte(raw, '@'); at >= 0 {
		raw = raw[:at]
	}
	slash := lastIndexByte(raw, '/')
	colon := lastIndexByte(raw, ':')
	return colon > slash
}

// lastIndexByte mirrors strings.LastIndexByte without pulling strings into
// this call site; keeps the helper local since hasExplicitTag is the only
// consumer.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// resolveCopyAllTagsDestination computes the "base" destination Ref (no
// tag) for --all-tags. The returned Ref's Tag field is intentionally empty;
// runCopyAllTagsPool fills it in per iteration via Ref.WithTag.
//
//nolint:gocritic // hugeParam: Ref uses value receivers uniformly per refname.go contract.
func resolveCopyAllTagsDestination(cmd *cobra.Command, args []string, src Ref, creds *options.RegistryCredentials) (Ref, error) {
	if len(args) >= 2 {
		// Reject an explicit dst tag for the same reason we reject an
		// explicit src tag: --all-tags implies the tag list comes from
		// the source, not the user.
		if hasExplicitTag(args[1]) {
			return Ref{}, cmdutil.UsageErrorf(cmd,
				"--all-tags is incompatible with a destination tag; remove the :tag suffix from %q", args[1])
		}
		ref, err := Normalize(args[1], creds)
		if err != nil {
			return Ref{}, &cmdutil.AgentError{
				Code:     kindRegistryInvalidReference,
				Message:  err.Error(),
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		// Clear whatever tag Normalize defaulted in; the per-tag fan-out
		// supplies the real tag.
		ref.Tag = ""
		ref.Digest = ""
		return ref, nil
	}
	if creds == nil || creds.Endpoint == "" || creds.ProjectID == "" {
		return Ref{}, &cmdutil.AgentError{
			Code:     kindRegistryNotConfigured,
			Message:  "Registry is not configured. Run `verda registry configure` first.",
			ExitCode: cmdutil.ExitAuth,
		}
	}
	fullRepo := src.Repository
	if src.Project != "" {
		fullRepo = src.Project + "/" + src.Repository
	}
	return Ref{
		Host:       creds.Endpoint,
		Project:    creds.ProjectID,
		Repository: fullRepo,
	}, nil
}

// runCopyAllTagsPool spins up imageJobs worker goroutines and distributes
// per-tag copy jobs across them. Partial-success semantics: a failure on
// one tag never cancels siblings, so the user always sees the full result
// set. Ctrl-C propagates through ctx.Done to abort in-flight writes.
//
// We use sync.WaitGroup + channels rather than errgroup because errgroup
// cancels every sibling on the first error, which would mask the failure
// mode we want to surface (e.g. "2 tags copied, 1 failed with AUTH").
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; contract uses value receivers uniformly (see refname.go).
func runCopyAllTagsPool(
	ctx context.Context,
	cancel context.CancelFunc,
	srcReg Registry,
	dstReg Registry,
	srcBase Ref,
	dstBase Ref,
	tags []string,
	imageJobs int,
	creds *options.RegistryCredentials,
	opts *copyOptions,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
) []copyJobResult {
	rows := make([]imageRow, len(tags))
	for i, t := range tags {
		rows[i] = imageRow{
			Ref:   srcBase.WithTag(t).String(),
			Dst:   dstBase.WithTag(t).String(),
			State: stateQueued,
		}
	}

	useTUI := shouldUseBubbleteaCopy(opts, f.OutputFormat(), ioStreams.ErrOut)
	var program *tea.Program
	if useTUI {
		model := newPushViewModel(creds.Endpoint, creds.ProjectID, rows, cancel)
		program = tea.NewProgram(model, tea.WithOutput(ioStreams.ErrOut))
	}

	jobs := make(chan copyJob)
	resultsCh := make(chan copyJobResult, len(tags))

	var wg sync.WaitGroup
	for w := 0; w < imageJobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				r := copyOneTag(ctx, program, srcReg, dstReg, srcBase, dstBase, j, opts, creds)
				resultsCh <- r
			}
		}()
	}

	// Producer: fans tags into jobs, closes the channel once exhausted.
	go func() {
		for i, t := range tags {
			select {
			case <-ctx.Done():
				// Stop enqueueing; workers drain whatever they already
				// picked up and we move on to shutdown.
				close(jobs)
				return
			case jobs <- copyJob{Index: i, Tag: t}:
			}
		}
		close(jobs)
	}()

	// Closer: waits for all workers, then closes resultsCh so the drainer
	// can terminate.
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Drainer: accumulates results in index order and pushes progress
	// events to the TUI (when enabled). Runs in a dedicated goroutine so
	// the main goroutine can drive program.Run synchronously.
	done := make(chan []copyJobResult, 1)
	go func() {
		results := make([]copyJobResult, len(tags))
		var completed int
		for r := range resultsCh {
			results[r.Index] = r
			completed++
			if program != nil {
				program.Send(pushResultMsg{Index: r.Index, Err: r.Err})
				program.Send(pushHeaderNoteMsg{
					Note: fmt.Sprintf("copied %d of %d tag%s", completed, len(tags), pluralS(len(tags))),
				})
			}
		}
		if program != nil {
			program.Quit()
		}
		done <- results
	}()

	if useTUI {
		if _, err := program.Run(); err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "copy: progress view error: %v\n", err)
		}
	}
	return <-done
}

// copyOneTag performs a single Read+Write transfer for a given tag and
// reports progress into the bubbletea program when one is supplied. Safe
// to call from multiple goroutines; all shared state lives on the Registry
// implementations, which the caller is responsible for making concurrency-
// safe (ggcrRegistry is; transport-level state is per-request).
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; contract uses value receivers uniformly (see refname.go).
func copyOneTag(
	ctx context.Context,
	program *tea.Program,
	srcReg Registry,
	dstReg Registry,
	srcBase Ref,
	dstBase Ref,
	job copyJob,
	opts *copyOptions,
	creds *options.RegistryCredentials,
) copyJobResult {
	srcRef := srcBase.WithTag(job.Tag)
	dstRef := dstBase.WithTag(job.Tag)
	res := copyJobResult{
		Index: job.Index,
		Tag:   job.Tag,
		Src:   srcRef.String(),
		Dst:   dstRef.String(),
	}

	img, err := srcReg.Read(ctx, res.Src)
	if err != nil {
		res.Err = translateError(err)
		return res
	}

	progressCh := make(chan v1.Update, 16)
	forwardDone := make(chan struct{})
	var last v1.Update
	var gotAny bool
	go func() {
		defer close(forwardDone)
		for u := range progressCh {
			last = u
			gotAny = true
			if program != nil {
				program.Send(pushProgressMsg{Index: job.Index, Update: u})
			}
		}
	}()

	start := time.Now()
	wo := WriteOptions{Jobs: opts.Jobs, Progress: progressCh}
	writeErr := dstReg.Write(ctx, res.Dst, img, wo)
	<-forwardDone
	res.Took = time.Since(start)

	if gotAny {
		res.Bytes = last.Complete
	}
	res.Err = translateErrorWithExpiry(writeErr, creds)
	return res
}

// summarizeCopyResults computes the aggregate counts for the structured
// payload and the exit-code decision.
func summarizeCopyResults(results []copyJobResult) allTagsSummary {
	s := allTagsSummary{Total: len(results)}
	for _, r := range results {
		if r.Err != nil {
			s.Failed++
		} else {
			s.Succeeded++
		}
	}
	return s
}

// buildAllTagsOutput assembles the structured --all-tags payload.
func buildAllTagsOutput(results []copyJobResult, summary allTagsSummary) allTagsOutput {
	out := allTagsOutput{
		Results: make([]copyTagResult, len(results)),
		Summary: summary,
	}
	for i, r := range results {
		row := copyTagResult{
			Tag:        r.Tag,
			Src:        r.Src,
			Dst:        r.Dst,
			Bytes:      r.Bytes,
			DurationMs: r.Took.Milliseconds(),
			Status:     copyStatusSucceeded,
		}
		if r.Err != nil {
			row.Status = copyStatusFailed
			row.Error = r.Err.Error()
			var ae *cmdutil.AgentError
			if errors.As(r.Err, &ae) {
				row.ErrorCode = ae.Code
			}
		}
		out.Results[i] = row
	}
	return out
}

// newPartialFailureError surfaces a non-zero exit code when any tag copy
// failed. We embed the summary counts as details so agent-mode consumers
// can dispatch on specifics without re-parsing the structured payload.
func newPartialFailureError(s allTagsSummary) error {
	return &cmdutil.AgentError{
		Code: kindRegistryCopyPartialFailure,
		Message: fmt.Sprintf("%d of %d tag copies failed",
			s.Failed, s.Total),
		Details: map[string]any{
			"total":     s.Total,
			"succeeded": s.Succeeded,
			"failed":    s.Failed,
		},
		ExitCode: cmdutil.ExitAPI,
	}
}
