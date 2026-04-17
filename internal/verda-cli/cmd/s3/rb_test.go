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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// rbFakeAPI is a minimal API implementation for the rb command tests.
type rbFakeAPI struct {
	API
	listObjectsPages  []*s3.ListObjectsV2Output // served in order
	listObjectsCalls  int
	deleteObjectsIns  []*s3.DeleteObjectsInput
	deleteBucketInput *s3.DeleteBucketInput
	deleteBucketCalls int
	listErr           error
	deleteObjectsErr  error
	deleteBucketErr   error
}

func (r *rbFakeAPI) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	if r.listObjectsCalls >= len(r.listObjectsPages) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
	}
	page := r.listObjectsPages[r.listObjectsCalls]
	r.listObjectsCalls++
	return page, nil
}

func (r *rbFakeAPI) DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	r.deleteObjectsIns = append(r.deleteObjectsIns, in)
	if r.deleteObjectsErr != nil {
		return nil, r.deleteObjectsErr
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func (r *rbFakeAPI) DeleteBucket(ctx context.Context, in *s3.DeleteBucketInput, opts ...func(*s3.Options)) (*s3.DeleteBucketOutput, error) {
	r.deleteBucketCalls++
	r.deleteBucketInput = in
	if r.deleteBucketErr != nil {
		return nil, r.deleteBucketErr
	}
	return &s3.DeleteBucketOutput{}, nil
}

func TestRb_Success(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &rbFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRb(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket called %d times, want 1", fake.deleteBucketCalls)
	}
	if aws.ToString(fake.deleteBucketInput.Bucket) != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", aws.ToString(fake.deleteBucketInput.Bucket))
	}
	if len(fake.deleteObjectsIns) != 0 {
		t.Errorf("DeleteObjects should not have been called without --force, got %d", len(fake.deleteObjectsIns))
	}
	got := out.String()
	if !strings.Contains(got, "my-bucket") || !strings.Contains(got, "removed") {
		t.Errorf("stdout missing expected confirmation:\n%s", got)
	}
}

func TestRb_AgentModeNeedsYes(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &rbFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	cmd := NewCmdRb(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected CONFIRMATION_REQUIRED in agent mode without --yes")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if ae.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("Code = %q, want CONFIRMATION_REQUIRED", ae.Code)
	}
	if fake.deleteBucketCalls != 0 {
		t.Errorf("DeleteBucket should not be called when confirmation is required, got %d", fake.deleteBucketCalls)
	}
}

func TestRb_ForceEmptiesBucket(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &rbFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("one.txt")},
					{Key: aws.String("two.txt")},
					{Key: aws.String("three.txt")},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRb(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket", "--force", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(fake.deleteObjectsIns) != 1 {
		t.Fatalf("DeleteObjects called %d times, want 1", len(fake.deleteObjectsIns))
	}
	del := fake.deleteObjectsIns[0]
	if del.Delete == nil || len(del.Delete.Objects) != 3 {
		t.Errorf("DeleteObjects batch size = %d, want 3", len(del.Delete.Objects))
	}
	if fake.deleteBucketCalls != 1 {
		t.Errorf("DeleteBucket called %d times, want 1", fake.deleteBucketCalls)
	}
}

func TestRb_InvalidURI(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	restore := withFakeClient(&rbFakeAPI{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRb(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://bucket/key"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when URI has a key component")
	}
	if !strings.Contains(err.Error(), "bucket URI") {
		t.Errorf("error should mention bucket URI, got: %v", err)
	}
}
