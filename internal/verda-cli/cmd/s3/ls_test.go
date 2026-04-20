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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// fakeS3API implements API — only the methods ls uses are meaningful.
// Leave the rest as "not implemented" returns.
type fakeS3API struct {
	API
	buckets        []s3types.Bucket
	objects        []s3types.Object
	commonPrefixes []s3types.CommonPrefix
	listBucketsErr error
	listObjectsErr error
}

func (f *fakeS3API) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	if f.listBucketsErr != nil {
		return nil, f.listBucketsErr
	}
	return &s3.ListBucketsOutput{Buckets: f.buckets}, nil
}

func (f *fakeS3API) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if f.listObjectsErr != nil {
		return nil, f.listObjectsErr
	}
	return &s3.ListObjectsV2Output{
		Contents:       f.objects,
		CommonPrefixes: f.commonPrefixes,
		IsTruncated:    aws.Bool(false),
	}, nil
}

func withFakeClient(fake API) (restore func()) {
	orig := clientBuilder
	clientBuilder = func(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (API, error) {
		return fake, nil
	}
	return func() { clientBuilder = orig }
}

func TestLs_Buckets_Human(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	when := time.Date(2026, 3, 15, 14, 22, 1, 0, time.UTC)
	fake := &fakeS3API{buckets: []s3types.Bucket{
		{Name: aws.String("bucket-one"), CreationDate: aws.Time(when)},
		{Name: aws.String("bucket-two"), CreationDate: aws.Time(when)},
	}}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLs(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"CREATED", "NAME", "bucket-one", "bucket-two"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestLs_Buckets_JSON(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{buckets: []s3types.Bucket{
		{Name: aws.String("b1"), CreationDate: aws.Time(time.Now())},
	}}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	cmd := NewCmdLs(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var payload struct {
		Buckets []struct {
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"buckets"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Buckets) != 1 || payload.Buckets[0].Name != "b1" {
		t.Errorf("unexpected buckets: %+v", payload.Buckets)
	}
}

func TestLs_Objects_WithPrefix(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{
		objects: []s3types.Object{
			{Key: aws.String("file1.txt"), Size: aws.Int64(1024), LastModified: aws.Time(time.Now())},
		},
		commonPrefixes: []s3types.CommonPrefix{
			{Prefix: aws.String("subdir1/")},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLs(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket/"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := out.String()
	for _, want := range []string{"KEY", "PRE", "subdir1/", "file1.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestLs_EmptyObjects(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLs(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://empty-bucket/"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No objects found") {
		t.Errorf("expected empty-state message, got: %s", out.String())
	}
}

func TestLs_InvalidURI(t *testing.T) {
	// no t.Parallel — still uses clientBuilder
	restore := withFakeClient(&fakeS3API{})
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLs(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"not-an-s3-uri"})
	cmd.SetContext(context.Background())
	// Silence cobra's error echo.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for non-s3 URI")
	}
}
