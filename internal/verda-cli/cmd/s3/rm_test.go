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
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// rmFakeAPI is a minimal API implementation for the rm command tests.
type rmFakeAPI struct {
	API
	listObjectsPages   []*s3.ListObjectsV2Output
	listObjectsCalls   int
	deleteObjectIns    []*s3.DeleteObjectInput
	deleteObjectCalls  int
	deleteObjectsIns   []*s3.DeleteObjectsInput
	deleteObjectsCalls int
	listErr            error
	deleteObjectErr    error
	deleteObjectsErr   error
}

func (r *rmFakeAPI) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
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

func (r *rmFakeAPI) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	r.deleteObjectCalls++
	r.deleteObjectIns = append(r.deleteObjectIns, in)
	if r.deleteObjectErr != nil {
		return nil, r.deleteObjectErr
	}
	return &s3.DeleteObjectOutput{}, nil
}

func (r *rmFakeAPI) DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	r.deleteObjectsCalls++
	r.deleteObjectsIns = append(r.deleteObjectsIns, in)
	if r.deleteObjectsErr != nil {
		return nil, r.deleteObjectsErr
	}
	return &s3.DeleteObjectsOutput{}, nil
}

// batchKeys extracts and sorts the keys from a DeleteObjects batch.
func batchKeys(in *s3.DeleteObjectsInput) []string {
	if in == nil || in.Delete == nil {
		return nil
	}
	keys := make([]string, 0, len(in.Delete.Objects))
	for _, o := range in.Delete.Objects {
		keys = append(keys, aws.ToString(o.Key))
	}
	sort.Strings(keys)
	return keys
}

func TestRm_SingleObject_Yes(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	fake := &rmFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket/path/to/obj.txt", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if fake.deleteObjectCalls != 1 {
		t.Fatalf("DeleteObject calls = %d, want 1", fake.deleteObjectCalls)
	}
	got := fake.deleteObjectIns[0]
	if aws.ToString(got.Bucket) != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", aws.ToString(got.Bucket))
	}
	if aws.ToString(got.Key) != "path/to/obj.txt" {
		t.Errorf("Key = %q, want path/to/obj.txt", aws.ToString(got.Key))
	}
	stdout := out.String()
	if !strings.Contains(stdout, "deleted") || !strings.Contains(stdout, "path/to/obj.txt") {
		t.Errorf("stdout missing deletion confirmation:\n%s", stdout)
	}
}

func TestRm_RecursiveWithFilter(t *testing.T) {
	// no t.Parallel
	// With include='*.txt' applied against the FULL key, matches a.txt and c.txt only
	// (nested/d.txt is excluded because '*' doesn't cross '/').
	fake := &rmFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("a.txt")},
					{Key: aws.String("b.log")},
					{Key: aws.String("c.txt")},
					{Key: aws.String("nested/d.txt")},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/", "--recursive", "--include", "*.txt", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.deleteObjectsCalls != 1 {
		t.Fatalf("DeleteObjects calls = %d, want 1", fake.deleteObjectsCalls)
	}
	keys := batchKeys(fake.deleteObjectsIns[0])
	want := []string{"a.txt", "c.txt"}
	if len(keys) != len(want) {
		t.Fatalf("deleted keys = %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("deleted keys[%d] = %q, want %q (all=%v)", i, keys[i], k, keys)
		}
	}
}

func TestRm_RecursiveWithExclude(t *testing.T) {
	// no t.Parallel
	fake := &rmFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("keep.txt")},
					{Key: aws.String("drop.log")},
					{Key: aws.String("also-keep.txt")},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/", "--recursive", "--exclude", "*.log", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.deleteObjectsCalls != 1 {
		t.Fatalf("DeleteObjects calls = %d, want 1", fake.deleteObjectsCalls)
	}
	keys := batchKeys(fake.deleteObjectsIns[0])
	want := []string{"also-keep.txt", "keep.txt"}
	if len(keys) != len(want) {
		t.Fatalf("deleted keys = %v, want %v", keys, want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("deleted keys[%d] = %q, want %q (all=%v)", i, keys[i], k, keys)
		}
	}
}

func TestRm_Dryrun(t *testing.T) {
	// no t.Parallel
	fake := &rmFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("one.txt")},
					{Key: aws.String("two.txt")},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/", "--recursive", "--dryrun"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.deleteObjectCalls != 0 {
		t.Errorf("DeleteObject should not be called in dryrun, got %d", fake.deleteObjectCalls)
	}
	if fake.deleteObjectsCalls != 0 {
		t.Errorf("DeleteObjects should not be called in dryrun, got %d", fake.deleteObjectsCalls)
	}
	stdout := out.String()
	for _, want := range []string{"(dry run) would delete", "one.txt", "two.txt"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestRm_AgentModeNeedsYes(t *testing.T) {
	// no t.Parallel
	fake := &rmFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/key"})
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
	if fake.deleteObjectCalls != 0 {
		t.Errorf("DeleteObject should not be called when confirmation required, got %d", fake.deleteObjectCalls)
	}
}

func TestRm_MissingKeyWithoutRecursive(t *testing.T) {
	// no t.Parallel
	restore := withFakeClient(&rmFakeAPI{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when key is missing and --recursive not set")
	}
	if !strings.Contains(err.Error(), "object key") && !strings.Contains(err.Error(), "recursive") {
		t.Errorf("error should mention missing key or --recursive, got: %v", err)
	}
}

func TestRm_InvalidURI(t *testing.T) {
	// no t.Parallel
	restore := withFakeClient(&rmFakeAPI{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdRm(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"not-an-s3-uri"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid URI")
	}
}
