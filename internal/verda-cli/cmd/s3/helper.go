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

	"github.com/aws/aws-sdk-go-v2/service/s3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// clientBuilder is a package-level var so tests can swap in fake API clients.
// Production code always calls the default via buildClient.
var clientBuilder = buildClientDefault

// buildClient returns a client satisfying the S3 API interface.
// Tests replace clientBuilder with a func returning a fake.
func buildClient(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (API, error) {
	return clientBuilder(ctx, f, ov)
}

func buildClientDefault(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (API, error) {
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
	return (*sdkS3Client)(sdkClient), nil
}

// sdkS3Client is a newtype alias around *s3.Client that satisfies the local
// API interface. Every method delegates directly to the underlying SDK client.
// This indirection keeps aws-sdk-go-v2 types from leaking into consumer
// packages that only need the API surface.
type sdkS3Client s3.Client

func (c *sdkS3Client) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return (*s3.Client)(c).ListBuckets(ctx, in, opts...)
}
func (c *sdkS3Client) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return (*s3.Client)(c).ListObjectsV2(ctx, in, opts...)
}
func (c *sdkS3Client) HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return (*s3.Client)(c).HeadBucket(ctx, in, opts...)
}
func (c *sdkS3Client) HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return (*s3.Client)(c).HeadObject(ctx, in, opts...)
}
func (c *sdkS3Client) GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return (*s3.Client)(c).GetObject(ctx, in, opts...)
}
func (c *sdkS3Client) PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return (*s3.Client)(c).PutObject(ctx, in, opts...)
}
func (c *sdkS3Client) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return (*s3.Client)(c).DeleteObject(ctx, in, opts...)
}
func (c *sdkS3Client) DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return (*s3.Client)(c).DeleteObjects(ctx, in, opts...)
}
func (c *sdkS3Client) CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return (*s3.Client)(c).CreateBucket(ctx, in, opts...)
}
func (c *sdkS3Client) DeleteBucket(ctx context.Context, in *s3.DeleteBucketInput, opts ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	return (*s3.Client)(c).DeleteBucket(ctx, in, opts...)
}
func (c *sdkS3Client) CopyObject(ctx context.Context, in *s3.CopyObjectInput, opts ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	return (*s3.Client)(c).CopyObject(ctx, in, opts...)
}
