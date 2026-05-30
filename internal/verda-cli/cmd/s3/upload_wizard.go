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
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	uploadBucketRootLabel = "(bucket root)"
	uploadNewBucketLabel  = "+ Create new bucket…"
	uploadNewFolderLabel  = "+ New folder…"
)

// upload wizard steps.
const (
	stepSource = iota
	stepBucket
	stepLocation
	stepConfirm
)

// runUploadWizard guides an interactive upload: source (validated to exist) ->
// destination bucket (pick or create) -> destination folder (root / existing /
// new) -> confirm. It then runs the normal upload path so large files still get
// the resumable multipart uploader; --recursive is inferred from the source.
//
// Navigation honors the hint bar: Esc steps BACK one question, Ctrl+C exits.
// The pickers return the raw prompter error so this loop can distinguish the two
// (cmdutil.IsPromptBack vs IsPromptInterrupt).
func runUploadWizard(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, args []string) error {
	// Unbounded: interactive prompts (user think-time) and the upload itself
	// must not hit the short per-request --timeout. Ctrl+C cancels.
	ctx := cmd.Context()

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	// When the source is supplied as an arg it is fixed (the source step just
	// returns it), so Esc on the bucket step exits rather than re-prompting.
	sourceFromArg := len(args) == 1

	var (
		source string
		isDir  bool
		bucket string
		prefix string
	)
	step := stepSource
	for {
		switch step {
		case stepSource:
			source, isDir, err = resolveUploadSource(ctx, f, ioStreams, args)
			if err != nil {
				if cmdutil.IsPromptCancel(err) {
					return nil // first step: any cancel exits
				}
				return err
			}
			if source == "" {
				return nil
			}
			step = stepBucket

		case stepBucket:
			bucket, err = selectBucketOrCreate(ctx, f, ioStreams, client)
			if back, exit, real := classifyNav(err, sourceFromArg); real != nil {
				return real
			} else if exit {
				return nil
			} else if back {
				step = stepSource
				continue
			}
			step = stepLocation

		case stepLocation:
			suggested := ""
			if isDir {
				suggested = filepath.Base(source)
			}
			prefix, err = selectUploadLocation(ctx, f, ioStreams, client, bucket, suggested)
			if back, exit, real := classifyNav(err, false); real != nil {
				return real
			} else if exit {
				return nil
			} else if back {
				step = stepBucket
				continue
			}
			step = stepConfirm

		case stepConfirm:
			dstURI := URI{Bucket: bucket, Key: prefix}
			opts.Recursive = isDir
			destDisplay := dstURI.String()
			if !strings.HasSuffix(destDisplay, "/") {
				destDisplay += "/"
			}
			preview := "verda s3 cp " + source + " " + destDisplay
			if isDir {
				preview += " --recursive"
			}
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Will run:  %s\n\n", preview)

			confirmed, cerr := f.Prompter().Confirm(ctx, "Proceed with upload? (esc to go back)", tui.WithConfirmDefault(true))
			if cerr != nil {
				if cmdutil.IsPromptBack(cerr) {
					step = stepLocation
					continue
				}
				if cmdutil.IsPromptInterrupt(cerr) {
					return nil
				}
				return cerr
			}
			if !confirmed {
				_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
				return nil
			}
			return runUpload(ctx, cmd, f, ioStreams, source, dstURI, opts)
		}
	}
}

// classifyNav maps a picker's returned error into back/exit/real outcomes.
// Esc (IsPromptBack) is a step-back unless this is the first interactive step,
// in which case it exits; Ctrl+C (IsPromptInterrupt) always exits. A non-prompt
// error is "real" and propagates. A nil error means advance.
func classifyNav(err error, firstStep bool) (back, exit bool, real error) {
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

// resolveUploadSource returns the local path to upload (from args[0] if given,
// else a prompt) and whether it is a directory. The path must exist; a bad
// explicit arg errors, a bad typed path re-prompts. On Esc/Ctrl+C it returns the
// raw prompter error; on empty input it returns ("", false, nil).
func resolveUploadSource(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) (string, bool, error) {
	if len(args) == 1 {
		info, err := os.Stat(args[0])
		if err != nil {
			return "", false, fmt.Errorf("source %q: %w", args[0], err)
		}
		return args[0], info.IsDir(), nil
	}

	for {
		path, err := f.Prompter().TextInput(ctx, "Local file or folder to upload")
		if err != nil {
			return "", false, err
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return "", false, nil
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %v — try again.\n", statErr)
			continue
		}
		return path, info.IsDir(), nil
	}
}

// selectBucketOrCreate lists buckets with a trailing "create new" choice and
// returns the chosen/created bucket name. Returns the raw prompter error if the
// top-level Select is canceled (so the caller can tell Esc from Ctrl+C); a
// canceled create-name sub-prompt loops back to the Select rather than exiting.
func selectBucketOrCreate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) (string, error) {
	out, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading buckets...", func() (*s3.ListBucketsOutput, error) {
		return client.ListBuckets(ctx, &s3.ListBucketsInput{})
	})
	if err != nil {
		return "", translateError(err)
	}

	for {
		labels := make([]string, 0, len(out.Buckets)+1)
		for i := range out.Buckets {
			labels = append(labels, "📦 "+aws.ToString(out.Buckets[i].Name))
		}
		labels = append(labels, uploadNewBucketLabel)

		idx, serr := f.Prompter().Select(ctx, "Destination bucket", labels, tui.WithShowHints(true))
		if serr != nil {
			return "", serr // raw: caller distinguishes Esc (back) from Ctrl+C (exit)
		}
		if idx != len(out.Buckets) {
			return aws.ToString(out.Buckets[idx].Name), nil
		}
		// Create new: Esc on the name prompt returns to the bucket list.
		name, cerr := createBucketInteractive(ctx, f, ioStreams, client)
		if cerr != nil {
			if cmdutil.IsPromptInterrupt(cerr) {
				return "", cerr
			}
			continue // Esc / empty -> back to the bucket list
		}
		if name != "" {
			return name, nil
		}
	}
}

// createBucketInteractive prompts for a name and creates the bucket. A canceled
// or empty prompt returns ("", err) / ("", nil) for the caller to loop on.
func createBucketInteractive(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) (string, error) {
	name, err := f.Prompter().TextInput(ctx, "New bucket name")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	_, err = cmdutil.WithSpinner(ctx, f.Status(), "Creating bucket...", func() (*s3.CreateBucketOutput, error) {
		return client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)})
	})
	if err != nil {
		return "", translateError(err)
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Created bucket %s\n", name)
	return name, nil
}

// selectUploadLocation returns the destination prefix: "" for the bucket root,
// an existing top-level folder, or a newly typed folder. Returns the raw
// prompter error if the Select is canceled; a canceled new-folder sub-prompt
// loops back to the Select.
func selectUploadLocation(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket, suggested string) (string, error) {
	payload, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading folders...", func() (objectsPayload, error) {
		return collectObjects(ctx, f, ioStreams, client, URI{Bucket: bucket}, "/")
	})
	if err != nil {
		return "", err
	}

	for {
		labels := []string{uploadBucketRootLabel}
		labels = append(labels, payload.CommonPrefixes...)
		labels = append(labels, uploadNewFolderLabel)

		idx, serr := f.Prompter().Select(ctx, "Destination folder in s3://"+bucket, labels, tui.WithShowHints(true))
		if serr != nil {
			return "", serr
		}
		switch {
		case idx == 0: // bucket root
			return "", nil
		case idx == len(labels)-1: // new folder
			name, nerr := newFolderInteractive(ctx, f, suggested)
			if nerr != nil {
				if cmdutil.IsPromptInterrupt(nerr) {
					return "", nerr
				}
				continue // Esc / empty -> back to the folder list
			}
			if name != "" {
				return name, nil
			}
		default:
			return payload.CommonPrefixes[idx-1], nil
		}
	}
}

// newFolderInteractive prompts for a new prefix segment, normalized to end in a
// single slash. Empty input returns "" (caller loops back to the folder list).
func newFolderInteractive(ctx context.Context, f cmdutil.Factory, suggested string) (string, error) {
	name, err := f.Prompter().TextInput(ctx, "New folder name", tui.WithDefault(suggested))
	if err != nil {
		return "", err
	}
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		return "", nil
	}
	return name + "/", nil
}
