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

package objectstorage

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// objectPickerCap bounds the flat object picker so a huge bucket can't produce
// an unusable Select. The user is told when the list is truncated.
const objectPickerCap = 1000

// selectBucket lists buckets and prompts the user to pick one. Returns the
// chosen bucket name, or ("", nil) on a clean cancel (Ctrl+C/Esc) or when no
// buckets exist — callers treat an empty name as "nothing to do, exit cleanly".
func selectBucket(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) (string, error) {
	out, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading buckets...", func() (*s3.ListBucketsOutput, error) {
		return client.ListBuckets(ctx, &s3.ListBucketsInput{})
	})
	if err != nil {
		return "", translateError(err)
	}
	if len(out.Buckets) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No buckets found.")
		return "", nil
	}

	labels := make([]string, len(out.Buckets))
	for i := range out.Buckets {
		labels[i] = aws.ToString(out.Buckets[i].Name)
	}
	idx, err := f.Prompter().Select(ctx, "Select bucket", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil
		}
		return "", err
	}
	return aws.ToString(out.Buckets[idx].Name), nil
}

// resolveBucketArg returns the s3:// argument a bucket-targeting command should
// act on, implementing the dual-mode contract:
//   - explicit positional arg  -> returned unchanged (param mode)
//   - omitted, --agent         -> structured MISSING_REQUIRED_FLAGS error
//   - omitted, non-TTY/piped   -> command help (no silent prompt in scripts)
//   - omitted, interactive TTY -> bucket picker, returns "s3://<chosen>"
//
// A clean cancel (Ctrl+C/Esc, or no buckets) returns ("", nil); callers should
// treat an empty string with a nil error as "exit cleanly, nothing to do".
func resolveBucketArg(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if f.AgentMode() {
		return "", cmdutil.NewMissingFlagsError([]string{"s3://bucket"})
	}
	// interactiveTTY also guards OutputFormat: `-o json` on a TTY must not launch
	// the picker, or a scripted caller gets an interactive session.
	if !interactiveTTY(f) {
		return "", cmd.Help()
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return "", err
	}
	bucket, err := selectBucket(ctx, f, ioStreams, client)
	if err != nil {
		return "", err
	}
	if bucket == "" {
		return "", nil
	}
	return "s3://" + bucket, nil
}

// resolveNewBucketArg returns the s3:// argument for a bucket-CREATING command.
// Like resolveBucketArg, but prompts for a NEW name (TextInput) rather than
// listing existing buckets. A clean cancel / empty input returns ("", nil) so
// callers treat it as "exit cleanly, nothing to do".
func resolveNewBucketArg(cmd *cobra.Command, f cmdutil.Factory, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	if f.AgentMode() {
		return "", cmdutil.NewMissingFlagsError([]string{"s3://bucket"})
	}
	if !interactiveTTY(f) {
		return "", cmd.Help()
	}
	name, err := promptNewBucketName(cmd.Context(), f)
	if err != nil || name == "" {
		return "", err
	}
	return "s3://" + name, nil
}

// promptNewBucketName asks for a bucket name and returns it trimmed. A clean
// cancel or empty input returns ("", nil).
func promptNewBucketName(ctx context.Context, f cmdutil.Factory) (string, error) {
	name, err := f.Prompter().TextInput(ctx, "New bucket name")
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(name), nil
}

type cappedKeys struct {
	keys      []string
	truncated bool
}

// listKeysCapped flat-lists up to limit object keys under bucket, returning
// truncated=true if the cap was hit before the listing finished.
func listKeysCapped(ctx context.Context, client API, bucket string, limit int) (keys []string, truncated bool, err error) {
	var token *string
	for {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}
		if token != nil {
			in.ContinuationToken = token
		}
		out, err := client.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, false, translateError(err)
		}
		for i := range out.Contents {
			keys = append(keys, aws.ToString(out.Contents[i].Key))
			if len(keys) >= limit {
				// Only truncated if more keys exist beyond this one (rest of this
				// page, or another page) — a bucket of exactly `limit` is complete.
				more := i < len(out.Contents)-1 || aws.ToBool(out.IsTruncated)
				return keys, more, nil
			}
		}
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			return keys, false, nil
		}
		token = out.NextContinuationToken
	}
}
