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

package s3

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// printMoveWizardIntro tells the user up front what the wizard will do — a move
// is a copy followed by deleting the source, so the heads-up matters.
func printMoveWizardIntro(ioStreams cmdutil.IOStreams) {
	title := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", title.Render("Move / rename an S3 object"))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n", dim.Render("Copies the object to a new location, then deletes the source."))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", dim.Render("Steps: pick source object → destination bucket → destination key → confirm.   Esc: back · Ctrl+C: cancel"))
}

// move wizard steps.
const (
	mvStepSourceBucket = iota
	mvStepSourceObject
	mvStepDestBucket
	mvStepDestKey
	mvStepConfirm
)

// moveStepTitles is indexed by the step constants above. Keep in sync.
var moveStepTitles = []string{"Source bucket", "Source object", "Destination bucket", "Destination key", "Confirm"}

// runMoveWizard guides an interactive S3->S3 move/rename as a stepped wizard.
// Every prompt is its own step, walked by an index into a steps slice, so Esc
// steps BACK exactly one prompt and Ctrl+C exits — the standard hint-bar
// contract. Steps the user can't act on (a source fixed by an argument) are
// dropped from the slice, so the "Step N of M" numbering always matches reality.
// On confirm it reuses the normal S3->S3 move path (CopyObject + delete).
//
// ctx is cmd.Context() (unbounded): the prompts involve user think-time and must
// not hit the short per-request --timeout.
func runMoveWizard(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, args []string) error {
	ctx := cmd.Context()
	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	srcBucket, srcKey, sourceFixed, err := parseMoveSourceArg(cmd, args)
	if err != nil {
		return err
	}
	var dstBucket, dstKey string

	printMoveWizardIntro(ioStreams)

	steps := buildMoveSteps(sourceFixed, srcBucket)
	// i < 0 (Esc on the first step) or i == len(steps) (done) ends the loop.
	for i := 0; i >= 0 && i < len(steps); {
		step := steps[i]
		printMoveStep(ioStreams, i, len(steps), step)

		switch step {
		case mvStepSourceBucket:
			b, perr := pickSourceBucket(ctx, f, ioStreams, client)
			if i, err = selectStep(i, b, perr, func() { srcBucket = b }); err != nil {
				return err
			}

		case mvStepSourceObject:
			k, perr := selectObjectKey(ctx, f, ioStreams, client, srcBucket)
			if i, err = selectStep(i, k, perr, func() { srcKey = k }); err != nil {
				return err
			}

		case mvStepDestBucket:
			b, perr := selectBucketOrCreate(ctx, f, ioStreams, client)
			// selectBucketOrCreate never returns an empty name without an error.
			if i, err = navIdx(i, perr, func() { dstBucket = b }); err != nil {
				return err
			}

		case mvStepDestKey:
			k, perr := f.Prompter().TextInput(ctx, "Destination key", tui.WithDefault(srcKey))
			i, err = navIdx(i, perr, func() {
				if k = strings.TrimSpace(k); k == "" {
					k = srcKey
				}
				dstKey = k
			})
			if err != nil {
				return err
			}

		case mvStepConfirm:
			done, cerr := moveConfirmStep(ctx, cmd, f, ioStreams, opts, URI{Bucket: srcBucket, Key: srcKey}, URI{Bucket: dstBucket, Key: dstKey})
			if cerr != nil || done {
				return cerr
			}
			i-- // not done (Esc or identical src/dst) -> back to the destination key
		}
	}
	return nil
}

// parseMoveSourceArg interprets an optional single s3:// argument: a full
// bucket/key fixes the source (sourceFixed=true); a bucket-only URI pre-fills
// srcBucket but still prompts for the object; no arg returns zeros.
func parseMoveSourceArg(cmd *cobra.Command, args []string) (srcBucket, srcKey string, sourceFixed bool, err error) {
	if len(args) != 1 {
		return "", "", false, nil
	}
	uri, perr := ParseS3URI(args[0])
	if perr != nil {
		return "", "", false, cmdutil.UsageErrorf(cmd, "invalid source %q: %v", args[0], perr)
	}
	if uri.Key != "" {
		return uri.Bucket, uri.Key, true, nil
	}
	return uri.Bucket, "", false, nil
}

// buildMoveSteps returns the ordered steps the user will walk, dropping any that
// an argument already satisfied: a full bucket/key source skips both source
// steps; a bucket-only source skips just the bucket step.
func buildMoveSteps(sourceFixed bool, srcBucket string) []int {
	var steps []int
	if !sourceFixed {
		if srcBucket == "" {
			steps = append(steps, mvStepSourceBucket)
		}
		steps = append(steps, mvStepSourceObject)
	}
	return append(steps, mvStepDestBucket, mvStepDestKey, mvStepConfirm)
}

// selectStep handles a list-picker step: an empty value with no error ends the
// wizard (no buckets/objects to act on), otherwise it delegates to navIdx.
func selectStep(i int, value string, perr error, apply func()) (next int, out error) {
	if perr == nil && value == "" {
		return -1, nil
	}
	return navIdx(i, perr, apply)
}

// navIdx advances the wizard index based on a prompter error: Esc steps back
// (i-1; -1 on the first step ends the loop = exit), Ctrl+C exits (returns a
// terminal index), a real error propagates, and success runs apply() then i+1.
func navIdx(i int, err error, apply func()) (next int, out error) {
	back, exit, fatal := classifyNav(err, false)
	switch {
	case fatal != nil:
		return i, fatal
	case exit:
		return -1, nil // terminate the loop without acting
	case back:
		return i - 1, nil
	default:
		apply()
		return i + 1, nil
	}
}

// moveConfirmStep previews the move and confirms it. done=false means step back
// to the key prompt (Esc, or an identical src/dst); done=true ends the wizard —
// the move ran, or the user exited/declined, with err carrying any real failure.
func moveConfirmStep(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, srcURI, dstURI URI) (done bool, err error) {
	if srcURI == dstURI {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "  Source and destination are identical — choose a different destination.")
		return false, nil
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Will run:  verda s3 mv %s %s\n\n", srcURI.String(), dstURI.String())
	confirmed, cerr := f.Prompter().Confirm(ctx, "Proceed with move? (esc to go back)", tui.WithConfirmDefault(true))
	back, exit, fatal := classifyNav(cerr, false)
	switch {
	case fatal != nil:
		return true, fatal
	case back:
		return false, nil // Esc -> step back to the key prompt
	case exit:
		return true, nil // Ctrl+C
	case !confirmed:
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return true, nil
	default:
		return true, runCopyMv(ctx, cmd, f, ioStreams, srcURI, dstURI, opts)
	}
}

// printMoveStep renders the "Step N of M · Title" header for the i-th step of n.
func printMoveStep(ioStreams cmdutil.IOStreams, i, n, step int) {
	header := fmt.Sprintf("Step %d of %d · %s", i+1, n, moveStepTitles[step])
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n", lipgloss.NewStyle().Bold(true).Render(header))
}
