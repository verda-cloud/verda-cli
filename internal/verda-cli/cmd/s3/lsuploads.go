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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// uploadEntry is the JSON/YAML shape for one in-progress multipart upload.
type uploadEntry struct {
	Key       string    `json:"key"        yaml:"key"`
	UploadID  string    `json:"upload_id"  yaml:"upload_id"`
	Initiated time.Time `json:"initiated"  yaml:"initiated"`
	Size      int64     `json:"size"       yaml:"size"`
}

// uploadsPayload is the top-level structured shape for ls-uploads.
type uploadsPayload struct {
	Uploads   []uploadEntry `json:"uploads"   yaml:"uploads"`
	Truncated bool          `json:"truncated" yaml:"truncated"`
}

type lsUploadsOptions struct {
	Prefix string
}

// NewCmdLsUploads builds the `verda s3 ls-uploads` cobra command.
func NewCmdLsUploads(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &lsUploadsOptions{}

	cmd := &cobra.Command{
		Use:   "ls-uploads s3://bucket",
		Short: "List in-progress (incomplete) multipart uploads",
		Long: cmdutil.LongDesc(`
			List in-progress multipart uploads in a bucket. The staged parts of
			an incomplete upload consume real storage and are billed even though
			the object does not appear in "verda s3 ls". This command surfaces
			that hidden cost: key, UploadId, when it was initiated, and the
			accumulated size of the parts uploaded so far.

			Use "verda s3 abort-uploads" to reclaim the storage.
		`),
		Example: cmdutil.Examples(`
			# List every in-progress upload in a bucket
			verda s3 ls-uploads s3://my-bucket

			# Only uploads under a key prefix
			verda s3 ls-uploads s3://my-bucket --prefix logs/

			# Machine-readable output
			verda s3 ls-uploads s3://my-bucket -o json
		`),
		// 0 args on a TTY launches the bucket picker; an explicit s3://bucket
		// runs directly. --agent errors; non-TTY shows help (no silent prompt).
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg, err := resolveBucketArg(cmd, f, ioStreams, args)
			if err != nil || arg == "" {
				return err
			}
			return runLsUploads(cmd, f, ioStreams, opts, arg)
		},
	}

	cmd.Flags().StringVar(&opts.Prefix, "prefix", "", "Only list uploads whose key starts with this prefix")

	return cmd
}

func runLsUploads(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *lsUploadsOptions, arg string) error {
	uri, err := ParseS3URI(arg)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	prefix := firstNonEmpty(opts.Prefix, uri.Key)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	payload, err := collectUploads(ctx, f, ioStreams, client, uri.Bucket, prefix)
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	renderUploads(ioStreams, payload)

	// On an interactive terminal, offer to resume one. The resume itself runs a
	// full upload, so it uses the unbounded cmd.Context() (not the listing ctx).
	interactive := interactiveTTY(f)
	if interactive && len(payload.Uploads) > 0 {
		return promptResumeFromUploads(cmd.Context(), f, ioStreams, client, uri.Bucket, payload.Uploads)
	}
	return nil
}

// collectUploads paginates ListMultipartUploads and, for each upload, sums its
// accumulated part size via ListParts. Guards against the truncated-with-empty-
// marker loop (same caveat as ListObjectsV2 in the s3 CLAUDE.md).
func collectUploads(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket, prefix string) (uploadsPayload, error) {
	payload := uploadsPayload{Uploads: []uploadEntry{}}

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Listing in-progress uploads...")
	}
	defer func() {
		if sp != nil {
			sp.Stop("")
		}
	}()

	var keyMarker, uploadIDMarker *string
	for {
		in := &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)}
		if prefix != "" {
			in.Prefix = aws.String(prefix)
		}
		if keyMarker != nil {
			in.KeyMarker = keyMarker
		}
		if uploadIDMarker != nil {
			in.UploadIdMarker = uploadIDMarker
		}

		out, err := client.ListMultipartUploads(ctx, in)
		if err != nil {
			return payload, translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
			fmt.Sprintf("ListMultipartUploads response: %d upload(s)", len(out.Uploads)), out)

		for i := range out.Uploads {
			key := aws.ToString(out.Uploads[i].Key)
			uploadID := aws.ToString(out.Uploads[i].UploadId)
			size, sizeErr := accumulatedPartSize(ctx, client, bucket, key, uploadID)
			if sizeErr != nil {
				return payload, sizeErr
			}
			payload.Uploads = append(payload.Uploads, uploadEntry{
				Key:       key,
				UploadID:  uploadID,
				Initiated: aws.ToTime(out.Uploads[i].Initiated),
				Size:      size,
			})
		}

		if !aws.ToBool(out.IsTruncated) {
			payload.Truncated = false
			return payload, nil
		}
		nextKey := aws.ToString(out.NextKeyMarker)
		nextUpload := aws.ToString(out.NextUploadIdMarker)
		if nextKey == "" && nextUpload == "" {
			payload.Truncated = true
			return payload, nil
		}
		keyMarker = out.NextKeyMarker
		uploadIDMarker = out.NextUploadIdMarker
	}
}

// accumulatedPartSize sums every part's size for one upload via ListParts.
// A NoSuchUpload (the upload was aborted/expired between the list and this call)
// is treated as zero rather than a fatal error.
func accumulatedPartSize(ctx context.Context, client API, bucket, key, uploadID string) (int64, error) {
	var (
		total  int64
		marker *string
	)
	for {
		in := &s3.ListPartsInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String(key),
			UploadId: aws.String(uploadID),
		}
		if marker != nil {
			in.PartNumberMarker = marker
		}
		out, err := client.ListParts(ctx, in)
		if err != nil {
			if isNoSuchUpload(err) {
				return total, nil
			}
			return 0, translateError(err)
		}
		for i := range out.Parts {
			total += aws.ToInt64(out.Parts[i].Size)
		}
		if !aws.ToBool(out.IsTruncated) || aws.ToString(out.NextPartNumberMarker) == "" {
			return total, nil
		}
		marker = out.NextPartNumberMarker
	}
}

func renderUploads(ioStreams cmdutil.IOStreams, payload uploadsPayload) {
	if len(payload.Uploads) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No in-progress multipart uploads.")
		return
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d in-progress upload(s)\n\n", len(payload.Uploads))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %-32s    %s\n", "INITIATED", "SIZE", "UPLOAD ID", "KEY")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %-32s    %s\n", "---------", "----", "---------", "---")
	for i := range payload.Uploads {
		u := &payload.Uploads[i]
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %-32s    %s\n",
			u.Initiated.UTC().Format(timestampLayout),
			humanBytes(u.Size),
			u.UploadID,
			u.Key,
		)
	}
	if payload.Truncated {
		_, _ = fmt.Fprintln(ioStreams.Out, "\n(results truncated)")
	}
}
