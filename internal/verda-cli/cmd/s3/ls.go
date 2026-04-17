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
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// timestampLayout renders S3 timestamps in a human-friendly form similar to
// the AWS CLI (`aws s3 ls`): "YYYY-MM-DD HH:MM:SS".
const timestampLayout = "2006-01-02 15:04:05"

// bucketEntry is the JSON/YAML shape emitted for a single bucket.
type bucketEntry struct {
	Name      string    `json:"name" yaml:"name"`
	CreatedAt time.Time `json:"created_at" yaml:"created_at"`
}

// bucketsPayload is the top-level JSON/YAML shape when listing buckets.
type bucketsPayload struct {
	Buckets []bucketEntry `json:"buckets" yaml:"buckets"`
}

// objectEntry is the JSON/YAML shape emitted for a single object.
type objectEntry struct {
	Key      string    `json:"key" yaml:"key"`
	Size     int64     `json:"size" yaml:"size"`
	Modified time.Time `json:"modified" yaml:"modified"`
	ETag     string    `json:"etag,omitempty" yaml:"etag,omitempty"`
}

// objectsPayload is the top-level JSON/YAML shape when listing objects.
type objectsPayload struct {
	Objects        []objectEntry `json:"objects" yaml:"objects"`
	CommonPrefixes []string      `json:"common_prefixes" yaml:"common_prefixes"`
	Truncated      bool          `json:"truncated" yaml:"truncated"`
}

type lsOptions struct {
	Recursive     bool
	HumanReadable bool
	Summarize     bool
}

// NewCmdLs creates the s3 ls cobra command.
func NewCmdLs(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &lsOptions{}

	cmd := &cobra.Command{
		Use:   "ls [s3://bucket[/prefix/]]",
		Short: "List buckets or objects in an S3 bucket",
		Long: cmdutil.LongDesc(`
			List buckets when invoked with no arguments, or list the
			top-level keys + common prefixes under an s3:// URI.

			Pass --recursive to flatten all keys under the prefix.
		`),
		Example: cmdutil.Examples(`
			# List all buckets
			verda s3 ls

			# List top-level keys under a bucket
			verda s3 ls s3://my-bucket

			# List keys under a prefix
			verda s3 ls s3://my-bucket/prefix/

			# Recursively list every key in the bucket
			verda s3 ls s3://my-bucket --recursive

			# Human-readable sizes + summary
			verda s3 ls s3://my-bucket --human-readable --summarize
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(cmd, f, ioStreams, opts, args)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Recursive, "recursive", false, "List all keys under the prefix (no delimiter)")
	flags.BoolVar(&opts.HumanReadable, "human-readable", false, "Display sizes in human-friendly units (KB, MB, ...)")
	flags.BoolVar(&opts.Summarize, "summarize", false, "Append a total objects/size footer")

	return cmd
}

func runLs(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *lsOptions, args []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return runLsBuckets(ctx, f, ioStreams, client)
	}

	uri, err := ParseS3URI(args[0])
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	return runLsObjects(ctx, f, ioStreams, client, uri, opts)
}

func runLsBuckets(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) error {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Listing buckets...")
	}

	out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("ListBuckets response: %d bucket(s)", len(out.Buckets)), out)

	payload := bucketsPayload{Buckets: make([]bucketEntry, 0, len(out.Buckets))}
	for i := range out.Buckets {
		payload.Buckets = append(payload.Buckets, bucketEntry{
			Name:      aws.ToString(out.Buckets[i].Name),
			CreatedAt: aws.ToTime(out.Buckets[i].CreationDate),
		})
	}

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return err
	}

	if len(payload.Buckets) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No buckets found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d bucket(s) found\n\n", len(payload.Buckets))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %s\n", "CREATED", "NAME")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %s\n", "-------", "----")
	for _, b := range payload.Buckets {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %s\n", b.CreatedAt.UTC().Format(timestampLayout), b.Name)
	}
	return nil
}

func runLsObjects(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, opts *lsOptions) error {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Listing objects...")
	}

	delimiter := "/"
	if opts.Recursive {
		delimiter = ""
	}

	payload, err := collectObjects(ctx, f, ioStreams, client, uri, delimiter)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	renderObjects(ioStreams, payload, opts)
	return nil
}

// collectObjects paginates ListObjectsV2 until exhausted or until the server
// stops returning a continuation token, aggregating everything into a single
// payload.
func collectObjects(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, delimiter string) (objectsPayload, error) {
	payload := objectsPayload{
		Objects:        []objectEntry{},
		CommonPrefixes: []string{},
	}

	var token *string
	for {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(uri.Bucket)}
		if uri.Key != "" {
			in.Prefix = aws.String(uri.Key)
		}
		if delimiter != "" {
			in.Delimiter = aws.String(delimiter)
		}
		if token != nil {
			in.ContinuationToken = token
		}

		out, err := client.ListObjectsV2(ctx, in)
		if err != nil {
			return payload, translateError(err)
		}

		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("ListObjectsV2 response: %d object(s), %d prefix(es)", len(out.Contents), len(out.CommonPrefixes)), out)
		appendPage(&payload, out)

		if !aws.ToBool(out.IsTruncated) {
			payload.Truncated = false
			return payload, nil
		}
		if out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			payload.Truncated = true
			return payload, nil
		}
		token = out.NextContinuationToken
	}
}

func appendPage(payload *objectsPayload, out *s3.ListObjectsV2Output) {
	for i := range out.Contents {
		payload.Objects = append(payload.Objects, objectEntry{
			Key:      aws.ToString(out.Contents[i].Key),
			Size:     aws.ToInt64(out.Contents[i].Size),
			Modified: aws.ToTime(out.Contents[i].LastModified),
			ETag:     aws.ToString(out.Contents[i].ETag),
		})
	}
	for i := range out.CommonPrefixes {
		payload.CommonPrefixes = append(payload.CommonPrefixes, aws.ToString(out.CommonPrefixes[i].Prefix))
	}
}

func renderObjects(ioStreams cmdutil.IOStreams, payload objectsPayload, opts *lsOptions) {
	if len(payload.Objects) == 0 && len(payload.CommonPrefixes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No objects found.")
		return
	}

	if len(payload.CommonPrefixes) > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %d object(s) found (+ %d common prefixes)\n\n", len(payload.Objects), len(payload.CommonPrefixes))
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %d object(s) found\n\n", len(payload.Objects))
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %s\n", "LAST MODIFIED", "SIZE", "KEY")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %s\n", "-------------", "----", "---")
	for _, p := range payload.CommonPrefixes {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %s\n", "", "PRE", p)
	}
	for _, o := range payload.Objects {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-19s    %-10s    %s\n",
			o.Modified.UTC().Format(timestampLayout),
			formatSize(o.Size, opts.HumanReadable),
			o.Key,
		)
	}

	if opts.Summarize {
		var totalSize int64
		for _, o := range payload.Objects {
			totalSize += o.Size
		}
		_, _ = fmt.Fprintln(ioStreams.Out)
		_, _ = fmt.Fprintf(ioStreams.Out, "Total objects: %d\n", len(payload.Objects))
		_, _ = fmt.Fprintf(ioStreams.Out, "Total size: %s\n", formatSize(totalSize, opts.HumanReadable))
	}
}

// formatSize returns the numeric size as a raw byte count or a human-readable
// binary-unit string (1 KB = 1024 B) when human is true.
func formatSize(size int64, human bool) string {
	if human {
		return humanBytes(size)
	}
	return strconv.FormatInt(size, 10)
}

// humanBytes formats a byte count using binary (1024-based) units, matching
// the convention used by `aws s3 ls --human-readable`.
//
//	humanBytes(0)      == "0 B"
//	humanBytes(1023)   == "1023 B"
//	humanBytes(1024)   == "1.0 KB"
//	humanBytes(1<<20)  == "1.0 MB"
func humanBytes(n int64) string {
	const unit = 1024
	if n < 0 {
		return "0 B"
	}
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	value := float64(n) / unit
	idx := 0
	for value >= unit && idx < len(units)-1 {
		value /= unit
		idx++
	}
	return fmt.Sprintf("%.1f %s", value, units[idx])
}
