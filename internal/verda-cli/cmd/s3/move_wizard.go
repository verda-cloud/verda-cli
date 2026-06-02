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
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// newBucketSentinel is the dest-bucket Choice value for "create a new bucket". A
// NUL byte can't be a bucket name, so it never collides with a real one.
const newBucketSentinel = "\x00new-bucket"

// moveWizardState holds the selections collected across the move wizard steps.
type moveWizardState struct {
	srcBucket    string
	srcKey       string
	dstBucket    string
	newDstBucket string // set when the user chose "create new bucket"
	dstKey       string
}

// runMoveWizard guides an interactive S3->S3 move/rename using the shared wizard
// engine (same progress bar + hint bar + exit-confirmation as `s3 configure`):
// source bucket → source object → destination bucket (pick or create) →
// destination key. A source fixed by an argument pre-sets and skips those steps.
// After the engine collects the selections it previews + confirms, creates the
// destination bucket if new, and runs the normal S3->S3 move (CopyObject + delete).
//
// ctx is cmd.Context() (unbounded): the prompts involve user think-time and must
// not hit the short per-request --timeout.
func runMoveWizard(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, args []string) error {
	ctx := cmd.Context()
	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	srcBucket, srcKey, _, err := parseMoveSourceArg(cmd, args)
	if err != nil {
		return err
	}
	st := &moveWizardState{srcBucket: srcBucket, srcKey: srcKey}

	engine := wizard.NewEngine(f.Prompter(), f.Status(),
		wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
	if err := engine.Run(ctx, buildMoveFlow(f, client, st)); err != nil {
		return err
	}

	return finalizeMove(ctx, cmd, f, ioStreams, client, opts, st)
}

// parseMoveSourceArg interprets an optional single s3:// argument: a full
// bucket/key fixes the source; a bucket-only URI pre-fills the bucket but still
// prompts for the object; no arg returns zeros. sourceFixed is informational —
// step skipping is driven by which of srcBucket/srcKey are non-empty.
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

// buildMoveFlow assembles the engine flow. Steps bound to a non-empty preset
// (source fixed by an arg) report IsSet and are skipped by the engine.
func buildMoveFlow(f cmdutil.Factory, client API, st *moveWizardState) *wizard.Flow {
	return &wizard.Flow{
		Name: "s3-move",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			moveStepSourceBucket(f, client, st),
			moveStepSourceObject(f, client, st),
			moveStepDestBucket(f, client, st),
			moveStepNewDestBucket(st),
			moveStepDestKey(st),
		},
	}
}

func moveStepSourceBucket(f cmdutil.Factory, client API, st *moveWizardState) wizard.Step {
	return wizard.Step{
		Name:        "source-bucket",
		Description: "Source bucket",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			return bucketChoices(ctx, f, client, false)
		},
		Setter: func(v any) {
			if s, _ := v.(string); s != "" {
				st.srcBucket = s
			}
		},
		Resetter: func() { st.srcBucket = "" },
		IsSet:    func() bool { return st.srcBucket != "" },
		Value:    func() any { return st.srcBucket },
	}
}

func moveStepSourceObject(f cmdutil.Factory, client API, st *moveWizardState) wizard.Step {
	return wizard.Step{
		Name:        "source-object",
		Description: "Source object",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"source-bucket"},
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			bucket, _ := store.Collected()["source-bucket"].(string)
			if bucket == "" {
				bucket = st.srcBucket
			}
			return objectChoices(ctx, f, client, bucket)
		},
		Setter:   func(v any) { st.srcKey, _ = v.(string) },
		Resetter: func() { st.srcKey = "" },
		IsSet:    func() bool { return st.srcKey != "" },
		Value:    func() any { return st.srcKey },
	}
}

func moveStepDestBucket(f cmdutil.Factory, client API, st *moveWizardState) wizard.Step {
	return wizard.Step{
		Name:        "dest-bucket",
		Description: "Destination bucket",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			return bucketChoices(ctx, f, client, true)
		},
		// Sentinel ("create new") leaves dstBucket empty; the new-bucket step sets
		// newDstBucket and finalizeMove creates it.
		Setter: func(v any) {
			if s, _ := v.(string); s != "" && s != newBucketSentinel {
				st.dstBucket = s
			}
		},
		Resetter: func() { st.dstBucket = "" },
		IsSet:    func() bool { return false },
		Value:    func() any { return st.dstBucket },
	}
}

func moveStepNewDestBucket(st *moveWizardState) wizard.Step {
	return wizard.Step{
		Name:        "new-dest-bucket",
		Description: "New bucket name",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"dest-bucket"},
		ShouldSkip: func(collected map[string]any) bool {
			v, _ := collected["dest-bucket"].(string)
			return v != newBucketSentinel
		},
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("bucket name cannot be empty")
			}
			return nil
		},
		Setter: func(v any) { st.newDstBucket = strings.TrimSpace(v.(string)) },
		// Clear on skip: picking an existing dest bucket must drop a stale name,
		// else it overrides the chosen bucket in finalizeMove.
		Resetter: func() { st.newDstBucket = "" },
		IsSet:    func() bool { return false },
		Value:    func() any { return st.newDstBucket },
	}
}

func moveStepDestKey(st *moveWizardState) wizard.Step {
	return wizard.Step{
		Name:        "dest-key",
		Description: "Destination key",
		Prompt:      wizard.TextInputPrompt,
		Required:    false, // blank → Default (the source key)
		DependsOn:   []string{"source-object"},
		Default: func(collected map[string]any) any {
			if k, _ := collected["source-object"].(string); k != "" {
				return k
			}
			return st.srcKey
		},
		Setter:   func(v any) { st.dstKey = strings.TrimSpace(v.(string)) },
		Resetter: func() { st.dstKey = "" },
		IsSet:    func() bool { return false },
		Value:    func() any { return st.dstKey },
	}
}

// bucketChoices lists buckets as wizard choices, optionally appending a trailing
// "create new bucket" option (for destination selection).
func bucketChoices(ctx context.Context, f cmdutil.Factory, client API, withCreate bool) ([]wizard.Choice, error) {
	out, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading buckets...", func() (*s3.ListBucketsOutput, error) {
		return client.ListBuckets(ctx, &s3.ListBucketsInput{})
	})
	if err != nil {
		return nil, translateError(err)
	}
	choices := make([]wizard.Choice, 0, len(out.Buckets)+1)
	for i := range out.Buckets {
		name := aws.ToString(out.Buckets[i].Name)
		choices = append(choices, wizard.Choice{Label: name, Value: name})
	}
	if withCreate {
		choices = append(choices, wizard.Choice{Label: "+ Create new bucket…", Value: newBucketSentinel})
	}
	return choices, nil
}

// objectChoices lists object keys in bucket (capped) as wizard choices. An empty
// bucket is an error — there is nothing to move out of it.
func objectChoices(ctx context.Context, f cmdutil.Factory, client API, bucket string) ([]wizard.Choice, error) {
	res, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading objects...", func() (cappedKeys, error) {
		k, truncated, e := listKeysCapped(ctx, client, bucket, objectPickerCap)
		return cappedKeys{keys: k, truncated: truncated}, e
	})
	if err != nil {
		return nil, err
	}
	if len(res.keys) == 0 {
		return nil, fmt.Errorf("no objects in s3://%s", bucket)
	}
	choices := make([]wizard.Choice, 0, len(res.keys))
	for _, k := range res.keys {
		choices = append(choices, wizard.Choice{Label: k, Value: k})
	}
	return choices, nil
}

// finalizeMove resolves the destination (creating a new bucket if requested),
// previews + confirms, and runs the S3->S3 move. An identical src/dst re-prompts
// the key rather than aborting. A clean cancel returns nil.
func finalizeMove(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, opts *cpOptions, st *moveWizardState) error {
	dstBucket := st.dstBucket
	if st.newDstBucket != "" {
		dstBucket = st.newDstBucket
	}
	dstKey := st.dstKey
	if dstKey == "" {
		dstKey = st.srcKey
	}
	srcURI := URI{Bucket: st.srcBucket, Key: st.srcKey}

	// Disallow moving onto itself; re-prompt the key until it differs.
	dstURI := URI{Bucket: dstBucket, Key: dstKey}
	for srcURI == dstURI {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "  Source and destination are identical — enter a different destination key.")
		k, kerr := f.Prompter().TextInput(ctx, "Destination key", tui.WithDefault(dstKey))
		if kerr != nil {
			if cmdutil.IsPromptCancel(kerr) {
				return nil
			}
			return kerr
		}
		if dstKey = strings.TrimSpace(k); dstKey == "" {
			dstKey = st.srcKey
		}
		dstURI = URI{Bucket: dstBucket, Key: dstKey}
	}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Will run:  verda s3 mv %s %s\n\n", srcURI.String(), dstURI.String())
	confirmed, cerr := f.Prompter().Confirm(ctx, "Proceed with move?", tui.WithConfirmDefault(true))
	if cerr != nil {
		if cmdutil.IsPromptCancel(cerr) {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
		return cerr
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}

	if st.newDstBucket != "" {
		if _, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating bucket...", func() (*s3.CreateBucketOutput, error) {
			return client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(dstBucket)})
		}); err != nil {
			return translateError(err)
		}
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Created bucket %s\n", dstBucket)
	}

	return runCopyMv(ctx, cmd, f, ioStreams, srcURI, dstURI, opts)
}
