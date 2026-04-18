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
	"path/filepath"

	"charm.land/lipgloss/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// previewCap is the maximum number of keys shown in the interactive preview
// before truncating with "... and N more".
const previewCap = 10

type rmOptions struct {
	Recursive bool
	Include   []string
	Exclude   []string
	Dryrun    bool
	Yes       bool
}

// rmError is the structured shape for a single object-level delete failure.
type rmError struct {
	Key   string `json:"key"   yaml:"key"`
	Error string `json:"error" yaml:"error"`
}

// rmPayload is the structured output shape for the rm command.
type rmPayload struct {
	Deleted     []string  `json:"deleted"                yaml:"deleted"`
	WouldDelete []string  `json:"would_delete,omitempty" yaml:"would_delete,omitempty"`
	Errors      []rmError `json:"errors"                 yaml:"errors"`
	Dryrun      bool      `json:"dryrun"                 yaml:"dryrun"`
}

// NewCmdRm builds the `verda s3 rm` cobra command.
func NewCmdRm(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &rmOptions{}

	cmd := &cobra.Command{
		Use:   "rm s3://bucket/key",
		Short: "Delete objects from an S3 bucket",
		Long: cmdutil.LongDesc(`
			Delete one or more objects from an S3 bucket.

			Without --recursive the URI must specify a single object key;
			with --recursive the URI is treated as a prefix and every
			matching object under it is deleted. --include and --exclude
			glob patterns filter the recursive set (matched against the
			full key; '*' does not cross '/'). Excludes take precedence
			over includes.

			Pass --dryrun to preview the set of keys without issuing any
			deletes. In --agent mode, --yes is required unless --dryrun
			is also passed.
		`),
		Example: cmdutil.Examples(`
			# Delete a single object
			verda s3 rm s3://my-bucket/path/to/obj.txt

			# Recursively delete every object under a prefix
			verda s3 rm s3://my-bucket/logs/ --recursive

			# Only delete .txt files, keep everything else
			verda s3 rm s3://my-bucket/data/ --recursive --include "*.txt"

			# Delete everything except .log files
			verda s3 rm s3://my-bucket/data/ --recursive --exclude "*.log"

			# Preview without deleting
			verda s3 rm s3://my-bucket/data/ --recursive --dryrun

			# Skip confirmation
			verda s3 rm s3://my-bucket/obj --yes
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRm(cmd, f, ioStreams, opts, args[0])
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Recursive, "recursive", false, "Treat the URI as a prefix and delete all matching objects")
	flags.StringArrayVar(&opts.Include, "include", nil, "Only delete keys matching this glob (repeatable)")
	flags.StringArrayVar(&opts.Exclude, "exclude", nil, "Skip keys matching this glob (repeatable, overrides --include)")
	flags.BoolVar(&opts.Dryrun, "dryrun", false, "Preview deletions without performing them")
	flags.BoolVar(&opts.Yes, "yes", false, "Skip confirmation prompt")

	return cmd
}

func runRm(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *rmOptions, arg string) error {
	uri, err := validateRmArgs(cmd, opts, arg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	// Destructive guard: agent mode requires --yes unless just previewing.
	if f.AgentMode() && !opts.Yes && !opts.Dryrun {
		return cmdutil.NewConfirmationRequiredError("rm")
	}

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	targets, err := gatherTargets(ctx, f, ioStreams, client, uri, opts)
	if err != nil {
		return err
	}

	if opts.Dryrun {
		return renderDryrun(f, ioStreams, uri, targets)
	}

	// Interactive confirmation (TTY path).
	if !opts.Yes && !f.AgentMode() {
		confirmed, confirmErr := confirmRm(ctx, f, ioStreams, uri, targets, opts.Recursive)
		if confirmErr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	return executeRm(ctx, f, ioStreams, client, uri, targets, opts.Recursive)
}

// validateRmArgs parses and validates the positional URI plus the flag
// combinations that require --recursive.
func validateRmArgs(cmd *cobra.Command, opts *rmOptions, arg string) (URI, error) {
	uri, err := ParseS3URI(arg)
	if err != nil {
		return URI{}, cmdutil.UsageErrorf(cmd, "%v", err)
	}
	if !opts.Recursive && uri.Key == "" {
		return URI{}, cmdutil.UsageErrorf(cmd, "rm requires an object key unless --recursive is set")
	}
	if (len(opts.Include) > 0 || len(opts.Exclude) > 0) && !opts.Recursive {
		return URI{}, cmdutil.UsageErrorf(cmd, "--include/--exclude require --recursive")
	}
	return uri, nil
}

// gatherTargets returns the full list of keys the command should act upon,
// paginating + filtering in recursive mode or wrapping the single key
// otherwise.
func gatherTargets(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, opts *rmOptions) ([]string, error) {
	if opts.Recursive {
		return listRmTargets(ctx, f, ioStreams, client, uri, opts.Include, opts.Exclude)
	}
	return []string{uri.Key}, nil
}

// renderDryrun emits the structured + human dry-run preview without issuing
// any SDK delete calls.
func renderDryrun(f cmdutil.Factory, ioStreams cmdutil.IOStreams, uri URI, targets []string) error {
	payload := rmPayload{
		Deleted:     []string{},
		WouldDelete: append([]string{}, targets...),
		Errors:      []rmError{},
		Dryrun:      true,
	}
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}
	for _, k := range targets {
		_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would delete s3://%s/%s\n", uri.Bucket, k)
	}
	if len(targets) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "(dry run) no objects match")
	}
	return nil
}

// executeRm performs the actual deletes and renders the final result.
func executeRm(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, targets []string, recursive bool) error {
	payload := rmPayload{Deleted: []string{}, Errors: []rmError{}, Dryrun: false}

	if len(targets) == 0 {
		if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
			return werr
		}
		_, _ = fmt.Fprintln(ioStreams.Out, "No objects matched; nothing to delete.")
		return nil
	}

	var err error
	if recursive {
		err = deleteBatched(ctx, f, ioStreams, client, uri.Bucket, targets, &payload)
	} else {
		err = deleteSingle(ctx, f, ioStreams, client, uri.Bucket, targets[0], &payload)
	}
	if err != nil {
		return err
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	for _, k := range payload.Deleted {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 deleted s3://%s/%s\n", uri.Bucket, k)
	}
	if len(payload.Deleted) > 1 {
		_, _ = fmt.Fprintf(ioStreams.Out, "%d objects deleted\n", len(payload.Deleted))
	}
	return nil
}

// listRmTargets paginates ListObjectsV2 under uri and filters the keys with
// the include/exclude glob patterns. Returned keys are in server order.
func listRmTargets(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, includes, excludes []string) ([]string, error) {
	var (
		sp    interface{ Stop(string) }
		keys  []string
		token *string
	)
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Listing objects...")
	}
	defer func() {
		if sp != nil {
			sp.Stop("")
		}
	}()

	for {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(uri.Bucket)}
		if uri.Key != "" {
			in.Prefix = aws.String(uri.Key)
		}
		if token != nil {
			in.ContinuationToken = token
		}
		out, err := client.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
			fmt.Sprintf("ListObjectsV2 response: %d object(s)", len(out.Contents)), out)

		for i := range out.Contents {
			k := aws.ToString(out.Contents[i].Key)
			if matchFilters(k, includes, excludes) {
				keys = append(keys, k)
			}
		}

		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			break
		}
		token = out.NextContinuationToken
	}
	return keys, nil
}

// matchFilters applies AWS-CLI-style include/exclude glob filters against a
// full key. Semantics: default-include when no includes; includes require at
// least one match; excludes always skip. Pattern errors are treated as
// no-match to keep the command robust against bad user input.
func matchFilters(key string, includes, excludes []string) bool {
	if len(includes) > 0 {
		matched := false
		for _, p := range includes {
			if ok, _ := filepath.Match(p, key); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, p := range excludes {
		if ok, _ := filepath.Match(p, key); ok {
			return false
		}
	}
	return true
}

// confirmRm shows the warning preamble + preview and asks the user to confirm.
func confirmRm(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, uri URI, targets []string, recursive bool) (bool, error) {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintln(ioStreams.ErrOut)

	if recursive {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n",
			warnStyle.Render(fmt.Sprintf("This will permanently delete %d object(s) under s3://%s/%s", len(targets), uri.Bucket, uri.Key)))
		preview := targets
		if len(preview) > previewCap {
			preview = preview[:previewCap]
		}
		for _, k := range preview {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "    - s3://%s/%s\n", uri.Bucket, k)
		}
		if more := len(targets) - len(preview); more > 0 {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "    \u2026 and %d more\n", more)
		}
		_, _ = fmt.Fprintln(ioStreams.ErrOut)
		prompt := fmt.Sprintf("Delete %d objects matching s3://%s/%s?", len(targets), uri.Bucket, uri.Key)
		return f.Prompter().Confirm(ctx, prompt)
	}

	key := ""
	if len(targets) > 0 {
		key = targets[0]
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n",
		warnStyle.Render(fmt.Sprintf("This will permanently delete s3://%s/%s", uri.Bucket, key)))
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	return f.Prompter().Confirm(ctx, fmt.Sprintf("Delete s3://%s/%s?", uri.Bucket, key))
}

// deleteSingle issues a single DeleteObject and updates payload accordingly.
func deleteSingle(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket, key string, payload *rmPayload) error {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Deleting s3://%s/%s...", bucket, key))
	}
	in := &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	out, err := client.DeleteObject(ctx, in)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return translateError(err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "DeleteObject response:", out)
	payload.Deleted = append(payload.Deleted, key)
	return nil
}

// deleteBatched issues DeleteObjects calls in chunks of up to maxDeleteBatch.
// Per-key failures are recorded in payload.Errors; transport-level errors
// abort the command with the original error wrapped via translateError.
func deleteBatched(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket string, keys []string, payload *rmPayload) error {
	for start := 0; start < len(keys); start += maxDeleteBatch {
		end := start + maxDeleteBatch
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[start:end]

		objs := make([]s3types.ObjectIdentifier, 0, len(chunk))
		for i := range chunk {
			objs = append(objs, s3types.ObjectIdentifier{Key: aws.String(chunk[i])})
		}

		in := &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{
				Objects: objs,
				Quiet:   aws.Bool(false),
			},
		}

		var sp interface{ Stop(string) }
		if status := f.Status(); status != nil {
			sp, _ = status.Spinner(ctx, fmt.Sprintf("Deleting %d object(s)...", len(chunk)))
		}
		out, err := client.DeleteObjects(ctx, in)
		if sp != nil {
			sp.Stop("")
		}
		if err != nil {
			return translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
			fmt.Sprintf("DeleteObjects response: batch of %d", len(chunk)), out)

		// Track per-object success / failure.
		failed := make(map[string]string, len(out.Errors))
		for i := range out.Errors {
			failed[aws.ToString(out.Errors[i].Key)] = aws.ToString(out.Errors[i].Message)
		}
		for _, k := range chunk {
			if msg, bad := failed[k]; bad {
				payload.Errors = append(payload.Errors, rmError{Key: k, Error: msg})
				continue
			}
			payload.Deleted = append(payload.Deleted, k)
		}
	}
	return nil
}
