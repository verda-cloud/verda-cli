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

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// mbPayload is the structured output shape for successful bucket creation.
type mbPayload struct {
	Bucket  string `json:"bucket" yaml:"bucket"`
	Created bool   `json:"created" yaml:"created"`
}

// NewCmdMb builds the `verda s3 mb` cobra command.
func NewCmdMb(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mb s3://bucket",
		Short: "Create (make) an S3 bucket",
		Long: cmdutil.LongDesc(`
			Create a new S3 bucket. The URI must be a bucket-only URI
			(s3://bucket) with no key component.
		`),
		Example: cmdutil.Examples(`
			# Create a new bucket
			verda s3 mb s3://my-new-bucket
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMb(cmd, f, ioStreams, args[0])
		},
	}

	return cmd
}

func runMb(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, arg string) error {
	uri, err := ParseS3URI(arg)
	if err != nil || uri.Key != "" {
		return cmdutil.UsageErrorf(cmd, "mb takes a bucket URI: s3://bucket")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	client, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Creating bucket %s...", uri.Bucket))
	}

	in := &s3.CreateBucketInput{Bucket: &uri.Bucket}
	out, err := client.CreateBucket(ctx, in)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "CreateBucket response:", out)

	payload := mbPayload{Bucket: uri.Bucket, Created: true}
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 created bucket s3://%s\n", uri.Bucket)
	return nil
}
