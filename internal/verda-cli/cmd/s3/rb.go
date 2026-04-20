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

	"charm.land/lipgloss/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// maxDeleteBatch is the per-request cap for S3 DeleteObjects batches.
const maxDeleteBatch = 1000

type rbOptions struct {
	Force bool
	Yes   bool
}

// rbPayload is the structured output shape for successful bucket removal.
type rbPayload struct {
	Bucket         string `json:"bucket" yaml:"bucket"`
	Removed        bool   `json:"removed" yaml:"removed"`
	ObjectsDeleted int    `json:"objects_deleted" yaml:"objects_deleted"`
}

// NewCmdRb builds the `verda s3 rb` cobra command.
func NewCmdRb(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &rbOptions{}

	cmd := &cobra.Command{
		Use:   "rb s3://bucket",
		Short: "Remove (delete) an S3 bucket",
		Long: cmdutil.LongDesc(`
			Remove an S3 bucket. The bucket must be empty unless --force is
			passed, in which case all objects are deleted first in batches.

			This action is destructive and cannot be undone. In --agent mode,
			--yes is required to proceed.
		`),
		Example: cmdutil.Examples(`
			# Remove an empty bucket
			verda s3 rb s3://my-bucket

			# Remove a non-empty bucket (deletes all objects first)
			verda s3 rb s3://my-bucket --force

			# Skip confirmation prompt
			verda s3 rb s3://my-bucket --yes
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRb(cmd, f, ioStreams, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Force, "force", false, "Empty the bucket before deletion")
	flags.BoolVar(&opts.Yes, "yes", false, "Skip confirmation prompt")

	return cmd
}

func runRb(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *rbOptions, arg string) error {
	uri, err := ParseS3URI(arg)
	if err != nil || uri.Key != "" {
		return cmdutil.UsageErrorf(cmd, "rb takes a bucket URI: s3://bucket")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	// Destructive guard: agent mode requires --yes.
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("rb")
	}

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	// Interactive confirmation (TTY path).
	if !opts.Yes && !f.AgentMode() {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n",
			warnStyle.Render(fmt.Sprintf("This will permanently delete bucket %q", uri.Bucket)))
		if opts.Force {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n",
				warnStyle.Render("...and ALL objects it contains"))
		}
		_, _ = fmt.Fprintln(ioStreams.ErrOut)

		confirmed, confirmErr := f.Prompter().Confirm(ctx, fmt.Sprintf("Delete bucket %q?", uri.Bucket))
		if confirmErr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	objectsDeleted := 0
	if opts.Force {
		n, err := emptyBucket(ctx, f, ioStreams, client, uri.Bucket)
		if err != nil {
			return err
		}
		objectsDeleted = n
	}

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Removing bucket %s...", uri.Bucket))
	}

	in := &s3.DeleteBucketInput{Bucket: &uri.Bucket}
	out, err := client.DeleteBucket(ctx, in)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "DeleteBucket response:", out)

	payload := rbPayload{Bucket: uri.Bucket, Removed: true, ObjectsDeleted: objectsDeleted}
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 removed bucket s3://%s\n", uri.Bucket)
	return nil
}

// emptyBucket paginates through all objects in a bucket and deletes them in
// batches of up to maxDeleteBatch. Returns the total number of objects deleted.
func emptyBucket(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket string) (int, error) {
	var (
		token   *string
		batch   []s3types.ObjectIdentifier
		deleted int
	)

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		in := &s3.DeleteObjectsInput{
			Bucket: &bucket,
			Delete: &s3types.Delete{
				Objects: batch,
				Quiet:   aws.Bool(true),
			},
		}
		out, err := client.DeleteObjects(ctx, in)
		if err != nil {
			return translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
			fmt.Sprintf("DeleteObjects response: batch of %d", len(batch)), out)
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Emptied batch of %d objects\n", len(batch))
		deleted += len(batch)
		batch = batch[:0]
		return nil
	}

	for {
		in := &s3.ListObjectsV2Input{Bucket: &bucket}
		if token != nil {
			in.ContinuationToken = token
		}
		out, err := client.ListObjectsV2(ctx, in)
		if err != nil {
			return deleted, translateError(err)
		}

		for i := range out.Contents {
			batch = append(batch, s3types.ObjectIdentifier{Key: out.Contents[i].Key})
			if len(batch) >= maxDeleteBatch {
				if err := flushBatch(); err != nil {
					return deleted, err
				}
			}
		}

		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			break
		}
		token = out.NextContinuationToken
	}

	if err := flushBatch(); err != nil {
		return deleted, err
	}
	return deleted, nil
}
