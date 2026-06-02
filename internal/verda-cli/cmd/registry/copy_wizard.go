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
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// copy wizard steps. Mirrors s3 cp's runUploadWizard step-machine: each step
// prompts, Esc steps back one, Ctrl+C exits, and a final confirm previews the
// equivalent command before running the real copy pipeline.
const (
	copyStepSource = iota
	copyStepAccess
	copyStepScope
	copyStepDest
	copyStepConfirm
)

// copyWizardEligible reports whether to launch the interactive copy wizard:
// an interactive terminal, not agent mode, and the default table output
// (json/yaml callers must pass <src> explicitly so scripts stay deterministic).
func copyWizardEligible(f cmdutil.Factory, ioStreams cmdutil.IOStreams) bool {
	return !f.AgentMode() && !isStructuredFormat(f.OutputFormat()) && isTerminalFn(ioStreams.Out)
}

// runCopyWizard guides an interactive copy: source image (validated) -> source
// access (public/anonymous/basic) -> scope (this tag / all tags) -> destination
// (pre-filled from the active VCR project) -> confirm. On confirm it runs the
// same pipeline as the flag path via runCopyResolved, so dry-run-free copies get
// the progress view, overwrite guard, and --all-tags fan-out unchanged.
//
// Navigation honors the hint bar: Esc steps BACK one question, Ctrl+C exits.
// Pickers return the raw prompter error so classifyWizardNav can tell the two
// apart.
//
//nolint:gocyclo // Interactive step machine with per-step back/exit navigation — inherently complex.
func runCopyWizard(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *copyOptions) error {
	// Unbounded: interactive think-time (and the copy itself, inside
	// runCopyResolved) must not hit the short per-request --timeout. Ctrl+C
	// cancels.
	ctx := cmd.Context()

	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		return checkExpiry(nil)
	}
	if err := checkExpiry(creds); err != nil {
		return err
	}
	if creds.Endpoint == "" || creds.ProjectID == "" {
		return &cmdutil.AgentError{
			Code:     kindRegistryNotConfigured,
			Message:  "Registry is not configured. Run `verda registry configure` first.",
			ExitCode: cmdutil.ExitAuth,
		}
	}

	prompter := f.Prompter()
	var (
		srcRaw      string
		srcRef      Ref
		srcPassword string
		dstRaw      string
	)
	step := copyStepSource
	for {
		switch step {
		case copyStepSource:
			raw, ref, perr := promptCopySource(ctx, prompter, ioStreams)
			if perr != nil {
				if cmdutil.IsPromptCancel(perr) {
					return nil // first step: any cancel exits
				}
				return perr
			}
			if raw == "" {
				return nil
			}
			srcRaw, srcRef = raw, ref
			step = copyStepAccess

		case copyStepAccess:
			pw, aerr := promptCopyAccess(ctx, prompter, opts)
			if back, exit, fatal := classifyWizardNav(aerr, false); fatal != nil {
				return fatal
			} else if exit {
				return nil
			} else if back {
				step = copyStepSource
				continue
			}
			srcPassword = pw
			step = copyStepScope

		case copyStepScope:
			serr := promptCopyScope(ctx, prompter, opts)
			if back, exit, fatal := classifyWizardNav(serr, false); fatal != nil {
				return fatal
			} else if exit {
				return nil
			} else if back {
				step = copyStepAccess
				continue
			}
			step = copyStepDest

		case copyStepDest:
			dst, derr := promptCopyDest(ctx, prompter, cmd, opts, srcRaw, srcRef, creds)
			if back, exit, fatal := classifyWizardNav(derr, false); fatal != nil {
				return fatal
			} else if exit {
				return nil
			} else if back {
				step = copyStepScope
				continue
			}
			dstRaw = dst
			step = copyStepConfirm

		case copyStepConfirm:
			back, cerr := confirmAndRunCopy(cmd, f, ioStreams, opts, creds, srcRaw, srcRef, srcPassword, dstRaw)
			if cerr != nil {
				return cerr
			}
			if back {
				step = copyStepDest
				continue
			}
			return nil
		}
	}
}

// classifyWizardNav maps a picker's returned error into back/exit/real outcomes.
// Esc (IsPromptBack) steps back; Ctrl+C (IsPromptInterrupt) exits; a non-prompt
// error propagates; nil advances. Mirrors s3 cp's classifyNav.
func classifyWizardNav(err error, firstStep bool) (back, exit bool, fatal error) {
	switch {
	case err == nil:
		return false, false, nil
	case cmdutil.IsPromptInterrupt(err):
		return false, true, nil
	case cmdutil.IsPromptBack(err):
		if firstStep {
			return false, true, nil
		}
		return true, false, nil
	default:
		return false, false, err
	}
}

// promptCopySource asks for the source image and validates it parses. A bad
// reference re-prompts (not a navigation error); Esc/Ctrl+C returns the raw
// prompter error; empty input returns ("", Ref{}, nil) so the caller exits.
func promptCopySource(ctx context.Context, prompter tui.Prompter, ioStreams cmdutil.IOStreams) (string, Ref, error) {
	for {
		raw, err := prompter.TextInput(ctx, "Source image (e.g. docker.io/library/nginx:1.25)")
		if err != nil {
			return "", Ref{}, err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", Ref{}, nil
		}
		ref, perr := Parse(raw)
		if perr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %v — try again.\n", perr)
			continue
		}
		return raw, ref, nil
	}
}

// promptCopyAccess selects the source-side authentication mode and, for the
// basic option, prompts for username + (masked) password. Sets opts.SrcAuth /
// opts.SrcUsername and returns the password (empty for the non-basic modes).
// Returns the raw prompter error from the Select so the caller can classify
// Esc (back) vs Ctrl+C (exit); a canceled basic sub-prompt loops back to the
// mode list rather than exiting.
func promptCopyAccess(ctx context.Context, prompter tui.Prompter, opts *copyOptions) (string, error) {
	const (
		accessPublic = iota
		accessAnonymous
		accessBasic
	)
	choices := []string{
		"Public / via `docker login` (default)",
		"Anonymous",
		"Username + password",
	}
	for {
		idx, serr := prompter.Select(ctx, "Source access", choices, tui.WithShowHints(true))
		if serr != nil {
			return "", serr // raw: caller classifies Esc vs Ctrl+C
		}
		switch idx {
		case accessPublic:
			opts.SrcAuth = srcAuthDockerConfig
			return "", nil
		case accessAnonymous:
			opts.SrcAuth = srcAuthAnonymous
			return "", nil
		case accessBasic:
			uname, pw, berr := promptBasicCreds(ctx, prompter)
			if berr != nil {
				if cmdutil.IsPromptInterrupt(berr) {
					return "", berr
				}
				continue // Esc / empty on a sub-prompt → back to the mode list
			}
			if uname == "" || pw == "" {
				continue // incomplete → back to the mode list
			}
			opts.SrcAuth = srcAuthBasic
			opts.SrcUsername = uname
			return pw, nil
		}
	}
}

// promptBasicCreds collects a username and masked password for --src-auth basic.
// Empty input (after a clean prompt) returns "" so the caller loops; a canceled
// prompt returns the raw error.
func promptBasicCreds(ctx context.Context, prompter tui.Prompter) (username, password string, err error) {
	u, err := prompter.TextInput(ctx, "Source username")
	if err != nil {
		return "", "", err
	}
	u = strings.TrimSpace(u)
	if u == "" {
		return "", "", nil
	}
	p, err := prompter.Password(ctx, "Source password")
	if err != nil {
		return "", "", err
	}
	return u, p, nil
}

// promptCopyScope asks whether to copy a single tag or every tag in the source
// repository, setting opts.AllTags. Returns the raw prompter error.
func promptCopyScope(ctx context.Context, prompter tui.Prompter, opts *copyOptions) error {
	idx, err := prompter.Select(ctx, "What to copy",
		[]string{"Just this image (tag)", "All tags in the repository"},
		tui.WithShowHints(true))
	if err != nil {
		return err
	}
	opts.AllTags = idx == 1
	return nil
}

// promptCopyDest asks for the destination, pre-filled with the synthesized VCR
// reference (single tag) or repository base (--all-tags). The user accepts it
// with Enter or edits it. Returns the raw prompter error.
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; contract uses value receivers uniformly (see refname.go).
func promptCopyDest(ctx context.Context, prompter tui.Prompter, cmd *cobra.Command, opts *copyOptions, srcRaw string, srcRef Ref, creds *options.RegistryCredentials) (string, error) {
	suggested := copyDestSuggestion(cmd, opts, srcRaw, srcRef, creds)
	label := "Destination"
	if opts.AllTags {
		label = "Destination repository (all tags)"
	}
	dst, err := prompter.TextInput(ctx, label, tui.WithDefault(suggested))
	if err != nil {
		return "", err
	}
	dst = strings.TrimSpace(dst)
	if dst == "" {
		dst = suggested
	}
	return dst, nil
}

// copyDestSuggestion computes the default destination shown in the dest step,
// reusing the same resolution the flag path uses. Falls back to "" if creds
// can't synthesize one (already guarded upstream, so this is defensive).
//
//nolint:gocritic // hugeParam: Ref uses value receivers uniformly (see refname.go).
func copyDestSuggestion(cmd *cobra.Command, opts *copyOptions, srcRaw string, srcRef Ref, creds *options.RegistryCredentials) string {
	if opts.AllTags {
		base, err := resolveCopyAllTagsDestination(cmd, []string{copyTaglessSource(srcRef)}, srcRef, creds)
		if err != nil {
			return ""
		}
		return base.String()
	}
	dst, err := resolveCopyDestination([]string{srcRaw}, srcRef, creds)
	if err != nil {
		return ""
	}
	return dst.String()
}

// copyTaglessSource renders the source as a tag-less repository path
// (<host>/<project>/<repo>) so --all-tags isn't rejected for carrying an
// explicit :tag the user may have typed.
//
//nolint:gocritic // hugeParam: Ref uses value receivers uniformly (see refname.go).
func copyTaglessSource(srcRef Ref) string {
	return srcRef.Host + "/" + srcRef.FullRepository()
}

// confirmAndRunCopy previews the equivalent command, confirms (default Yes), and
// runs the real copy via runCopyResolved. back=true means Esc -> return to the
// destination step; Ctrl+C or "no" is a clean exit (back=false, err=nil).
//
//nolint:gocritic // hugeParam: Ref uses value receivers uniformly (see refname.go).
func confirmAndRunCopy(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *copyOptions, creds *options.RegistryCredentials, srcRaw string, srcRef Ref, srcPassword, dstRaw string) (back bool, err error) {
	// Build the positional args the resolved copy expects. --all-tags needs a
	// tag-less source (and destination); single-tag passes the source verbatim.
	srcArg := srcRaw
	if opts.AllTags {
		srcArg = copyTaglessSource(srcRef)
	}
	args := []string{srcArg, dstRaw}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Will run:  %s\n\n", copyCommandPreview(args, opts))

	confirmed, cerr := f.Prompter().Confirm(cmd.Context(), "Proceed with copy? (esc to go back)", tui.WithConfirmDefault(true))
	if cerr != nil {
		if cmdutil.IsPromptBack(cerr) {
			return true, nil
		}
		if cmdutil.IsPromptInterrupt(cerr) {
			return false, nil
		}
		return false, cerr
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return false, nil
	}

	srcAuth, aerr := buildSourceAuth(opts, srcPassword)
	if aerr != nil {
		return false, aerr
	}
	return false, runCopyResolved(cmd, f, ioStreams, opts, args, creds, srcRef, srcAuth)
}

// copyCommandPreview renders the equivalent `verda registry copy …` command so
// the user sees exactly what the wizard will run (and can reproduce it later).
func copyCommandPreview(args []string, opts *copyOptions) string {
	var b strings.Builder
	b.WriteString("verda registry copy ")
	b.WriteString(strings.Join(args, " "))
	if opts.AllTags {
		b.WriteString(" --all-tags")
	}
	if opts.SrcAuth != "" && opts.SrcAuth != srcAuthDockerConfig {
		b.WriteString(" --src-auth ")
		b.WriteString(opts.SrcAuth)
	}
	if opts.SrcAuth == srcAuthBasic {
		b.WriteString(" --src-username ")
		b.WriteString(opts.SrcUsername)
		b.WriteString(" --src-password-stdin")
	}
	return b.String()
}
