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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

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
	if !cmdutil.IsStdoutTerminal() {
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
