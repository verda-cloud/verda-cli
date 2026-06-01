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
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// abortedUpload is the structured shape for one aborted multipart upload.
type abortedUpload struct {
	Key      string `json:"key"       yaml:"key"`
	UploadID string `json:"upload_id" yaml:"upload_id"`
}

// abortUploadsPayload is the structured output shape for abort-uploads.
type abortUploadsPayload struct {
	Aborted []abortedUpload `json:"aborted" yaml:"aborted"`
}

type abortUploadsOptions struct {
	OlderThan string
	Key       string
	Prefix    string
	Yes       bool
}

// NewCmdAbortUploads builds the `verda s3 abort-uploads` cobra command.
func NewCmdAbortUploads(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &abortUploadsOptions{}

	cmd := &cobra.Command{
		Use:   "abort-uploads s3://bucket",
		Short: "Abort in-progress multipart uploads to reclaim storage",
		Long: cmdutil.LongDesc(`
			Abort in-progress (incomplete) multipart uploads in a bucket. The
			staged parts of an incomplete upload consume real, billed storage
			even though the object never appears in "verda s3 ls". Aborting
			reclaims that storage.

			Use --older-than to abort only uploads initiated before a given age
			(e.g. 7d, 12h), and --key to target a single object key. Without
			either, EVERY in-progress upload in the bucket is aborted.

			This is destructive: aborted uploads cannot be resumed and their
			parts are deleted. In --agent mode, --yes is required.
		`),
		Example: cmdutil.Examples(`
			# Abort uploads older than 7 days
			verda s3 abort-uploads s3://my-bucket --older-than 7d

			# Abort every in-progress upload (with confirmation)
			verda s3 abort-uploads s3://my-bucket

			# Abort uploads for a single key
			verda s3 abort-uploads s3://my-bucket --key path/to/big.bin

			# Skip the confirmation prompt
			verda s3 abort-uploads s3://my-bucket --older-than 30d --yes
		`),
		// 0 args on a TTY launches the bucket picker; an explicit s3://bucket
		// runs directly. --agent errors; non-TTY shows help (no silent prompt).
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg, err := resolveBucketArg(cmd, f, ioStreams, args)
			if err != nil || arg == "" {
				return err
			}
			return runAbortUploads(cmd, f, ioStreams, opts, arg)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.OlderThan, "older-than", "", "Only abort uploads initiated before this age (e.g. 7d, 12h)")
	flags.StringVar(&opts.Key, "key", "", "Only abort uploads for this exact object key")
	flags.StringVar(&opts.Prefix, "prefix", "", "Only abort uploads whose key starts with this prefix")
	flags.BoolVar(&opts.Yes, "yes", false, "Skip confirmation prompt")

	return cmd
}

func runAbortUploads(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *abortUploadsOptions, arg string) error {
	uri, err := ParseS3URI(arg)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	age, err := parseOlderThan(opts.OlderThan)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	// Destructive guard: agent mode requires --yes.
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("abort-uploads")
	}

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	prefix := firstNonEmpty(opts.Prefix, uri.Key)
	listed, err := collectUploads(ctx, f, ioStreams, client, uri.Bucket, prefix)
	if err != nil {
		return err
	}

	targets := filterAbortTargets(listed.Uploads, opts.Key, age)
	if len(targets) == 0 {
		if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), abortUploadsPayload{Aborted: []abortedUpload{}}); wrote {
			return werr
		}
		_, _ = fmt.Fprintln(ioStreams.Out, "No matching in-progress uploads to abort.")
		return nil
	}

	if !opts.Yes && !f.AgentMode() {
		confirmed, confirmErr := confirmAbort(ctx, f, ioStreams, uri.Bucket, targets)
		if confirmErr != nil {
			if cmdutil.IsPromptCancel(confirmErr) {
				_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
				return nil
			}
			return confirmErr
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	return executeAbort(ctx, f, ioStreams, client, uri.Bucket, targets)
}

// filterAbortTargets narrows the listed uploads to those matching --key (exact)
// and --older-than (initiated before now-age). A zero age means no age filter.
func filterAbortTargets(uploads []uploadEntry, key string, age time.Duration) []uploadEntry {
	var cutoff time.Time
	if age > 0 {
		cutoff = time.Now().Add(-age)
	}
	targets := make([]uploadEntry, 0, len(uploads))
	for i := range uploads {
		if key != "" && uploads[i].Key != key {
			continue
		}
		if age > 0 && !uploads[i].Initiated.Before(cutoff) {
			continue
		}
		targets = append(targets, uploads[i])
	}
	return targets
}

// confirmAbort prints the destructive warning + preview and asks to confirm.
func confirmAbort(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, bucket string, targets []uploadEntry) (bool, error) {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n",
		warnStyle.Render(fmt.Sprintf("This will abort %d in-progress upload(s) in s3://%s and delete their staged parts", len(targets), bucket)))

	preview := targets
	if len(preview) > previewCap {
		preview = preview[:previewCap]
	}
	for i := range preview {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "    - s3://%s/%s (%s)\n", bucket, preview[i].Key, preview[i].UploadID)
	}
	if more := len(targets) - len(preview); more > 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "    … and %d more\n", more)
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	return f.Prompter().Confirm(ctx, fmt.Sprintf("Abort %d upload(s) in s3://%s?", len(targets), bucket))
}

// executeAbort issues an AbortMultipartUpload per target and renders results.
func executeAbort(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket string, targets []uploadEntry) error {
	payload := abortUploadsPayload{Aborted: make([]abortedUpload, 0, len(targets))}

	for i := range targets {
		var sp interface{ Stop(string) }
		if status := f.Status(); status != nil {
			sp, _ = status.Spinner(ctx, fmt.Sprintf("Aborting s3://%s/%s...", bucket, targets[i].Key))
		}
		out, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String(targets[i].Key),
			UploadId: aws.String(targets[i].UploadID),
		})
		if sp != nil {
			sp.Stop("")
		}
		if err != nil {
			// An upload aborted/expired between list and now is already gone.
			if isNoSuchUpload(err) {
				payload.Aborted = append(payload.Aborted, abortedUpload{Key: targets[i].Key, UploadID: targets[i].UploadID})
				continue
			}
			return translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "AbortMultipartUpload response:", out)
		payload.Aborted = append(payload.Aborted, abortedUpload{Key: targets[i].Key, UploadID: targets[i].UploadID})
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	for i := range payload.Aborted {
		_, _ = fmt.Fprintf(ioStreams.Out, "✓ aborted s3://%s/%s\n", bucket, payload.Aborted[i].Key)
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "%d upload(s) aborted\n", len(payload.Aborted))
	return nil
}
