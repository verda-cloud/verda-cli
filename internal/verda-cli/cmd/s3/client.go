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
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// DefaultEndpoint is the bundled Verda S3 endpoint used when the user has not
// configured one explicitly. Update when the production URL is confirmed.
const DefaultEndpoint = "https://s3.verda.cloud"

// API is the minimal subset of the AWS S3 client used by this package.
// It exists to allow tests to inject fake clients without depending on
// aws-sdk-go-v2 internals.
type API interface {
	ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error)
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	HeadBucket(ctx context.Context, in *s3.HeadBucketInput, opts ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	DeleteBucket(ctx context.Context, in *s3.DeleteBucketInput, opts ...func(*s3.Options)) (*s3.DeleteBucketOutput, error)
	CopyObject(ctx context.Context, in *s3.CopyObjectInput, opts ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
}

// ClientOverrides captures per-invocation flag overrides for S3 client construction.
type ClientOverrides struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
}

// NewClient builds an *s3.Client from stored credentials + optional flag overrides.
// It returns a wrapped friendly error if no credentials are configured or if
// auth-mode is set to an unsupported value.
func NewClient(ctx context.Context, creds *options.S3Credentials, authMode string, ov ClientOverrides) (*s3.Client, error) {
	if err := validateAuthMode(authMode); err != nil {
		return nil, err
	}

	accessKey := firstNonEmpty(ov.AccessKey, creds.AccessKey)
	secretKey := firstNonEmpty(ov.SecretKey, creds.SecretKey)
	region := firstNonEmpty(ov.Region, creds.Region, "us-east-1")
	endpoint := resolveEndpoint(creds, ov.Endpoint)

	if accessKey == "" || secretKey == "" {
		return nil, errors.New("no S3 credentials configured\n\n" +
			"Run \"verda s3 configure\" to set up S3 access")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	}), nil
}

func resolveEndpoint(creds *options.S3Credentials, flag string) string {
	if flag != "" {
		return flag
	}
	if creds != nil && creds.Endpoint != "" {
		return creds.Endpoint
	}
	return DefaultEndpoint
}

func validateAuthMode(mode string) error {
	switch mode {
	case "", "credentials":
		return nil
	case "api":
		return errors.New("API-based S3 auth not yet supported; set verda_s3_auth_mode=credentials")
	default:
		return fmt.Errorf("unknown verda_s3_auth_mode %q; expected \"credentials\" or \"api\"", mode)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
