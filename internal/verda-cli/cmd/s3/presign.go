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

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Presigner is the minimal interface over *s3.PresignClient used by the
// presign command. Tests can swap in fakes to avoid real AWS calls.
type Presigner interface {
	PresignGetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

// presignerBuilder is a package-level var so tests can inject a fake Presigner.
// Production code always calls the default via this indirection.
var presignerBuilder = defaultPresignerBuilder

func defaultPresignerBuilder(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (Presigner, error) {
	profile := f.Options().AuthOptions.Profile
	path, err := resolveCredentialsFile("")
	if err != nil {
		return nil, err
	}

	creds, err := options.LoadS3CredentialsForProfile(path, profile)
	if err != nil {
		// Missing file / missing profile: still try with an empty credentials struct
		// so NewClient's friendly error fires (pointing users at `verda s3 configure`).
		creds = &options.S3Credentials{}
	}

	sdkClient, err := NewClient(ctx, creds, creds.AuthMode, ov)
	if err != nil {
		return nil, err
	}
	return s3.NewPresignClient(sdkClient), nil
}

// presignPayload is the JSON/YAML shape emitted when -o structured output is
// requested.
type presignPayload struct {
	URL       string    `json:"url" yaml:"url"`
	ExpiresAt time.Time `json:"expires_at" yaml:"expires_at"`
}

type presignOptions struct {
	ExpiresIn time.Duration
}

// NewCmdPresign builds the `verda s3 presign` cobra command.
func NewCmdPresign(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &presignOptions{ExpiresIn: time.Hour}

	cmd := &cobra.Command{
		Use:   "presign s3://bucket/key",
		Short: "Generate a presigned GET URL for an S3 object",
		Long: cmdutil.LongDesc(`
			Generate a time-limited HTTPS URL that grants GET access to an
			object without exposing your S3 credentials. The URL expires after
			the duration given by --expires-in (default 1h).
		`),
		Example: cmdutil.Examples(`
			verda s3 presign s3://my-bucket/report.csv
			verda s3 presign s3://my-bucket/report.csv --expires-in 15m
			verda s3 presign s3://my-bucket/report.csv --expires-in 24h
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPresign(cmd, f, ioStreams, args[0], opts)
		},
	}

	cmd.Flags().DurationVar(&opts.ExpiresIn, "expires-in", opts.ExpiresIn, "URL expiration (e.g. 15m, 1h, 24h)")
	return cmd
}

func runPresign(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, arg string, opts *presignOptions) error {
	uri, err := ParseS3URI(arg)
	if err != nil || uri.Key == "" {
		return cmdutil.UsageErrorf(cmd, "presign requires an object URI: s3://bucket/key")
	}
	if opts.ExpiresIn <= 0 {
		return cmdutil.UsageErrorf(cmd, "--expires-in must be positive")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	presigner, err := presignerBuilder(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	in := &s3.GetObjectInput{Bucket: &uri.Bucket, Key: &uri.Key}
	expiresAt := time.Now().Add(opts.ExpiresIn)

	signed, err := presigner.PresignGetObject(ctx, in, s3.WithPresignExpires(opts.ExpiresIn))
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "PresignGetObject:", struct {
		Input     *s3.GetObjectInput `json:"input"`
		ExpiresIn string             `json:"expires_in"`
		URL       string             `json:"url"`
	}{Input: in, ExpiresIn: opts.ExpiresIn.String(), URL: signed.URL})

	payload := presignPayload{URL: signed.URL, ExpiresAt: expiresAt}
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	// Human output: URL alone on stdout (pipeable), context on stderr.
	_, _ = fmt.Fprintln(ioStreams.Out, signed.URL)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "URL expires at %s (in %s)\n",
		expiresAt.UTC().Format(time.RFC3339), opts.ExpiresIn)
	return nil
}
