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

//go:build !windows

package s3

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestInferPartSize(t *testing.T) {
	t.Parallel()
	if got := inferPartSize(nil); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	parts := []s3types.Part{
		{Size: aws.Int64(minPartSize)},
		{Size: aws.Int64(minPartSize)},
		{Size: aws.Int64(123)}, // short final part
	}
	if got := inferPartSize(parts); got != minPartSize {
		t.Errorf("inferPartSize = %d, want %d (largest)", got, minPartSize)
	}
}

func TestFindCheckpointByUploadID(t *testing.T) {
	withTempVerdaHome(t)
	cp1 := &checkpoint{UploadID: "u1", Bucket: "b", Key: "k1", AbsPath: "/tmp/a", MTime: time.Now().UTC()}
	cp2 := &checkpoint{UploadID: "u2", Bucket: "b", Key: "k2", AbsPath: "/tmp/b", MTime: time.Now().UTC()}
	if err := saveCheckpoint(uploadIdentity("/tmp/a", "b", "k1"), cp1); err != nil {
		t.Fatal(err)
	}
	if err := saveCheckpoint(uploadIdentity("/tmp/b", "b", "k2"), cp2); err != nil {
		t.Fatal(err)
	}

	got, err := findCheckpointByUploadID("u2")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got == nil || got.AbsPath != "/tmp/b" {
		t.Errorf("got %+v, want the u2 checkpoint (/tmp/b)", got)
	}
	if miss, _ := findCheckpointByUploadID("nope"); miss != nil {
		t.Errorf("unexpected match for unknown upload id: %+v", miss)
	}
}

// resumeFakeAPI serves a fixed set of pre-existing parts and records uploads /
// completion. CreateMultipartUpload must NOT be called (resume adopts the id).
type resumeFakeAPI struct {
	API
	existing    []s3types.Part
	createCalls int
	uploaded    []int32
	completed   []s3types.CompletedPart
}

func (r *resumeFakeAPI) CreateMultipartUpload(ctx context.Context, in *s3.CreateMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	r.createCalls++
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String("should-not-happen")}, nil
}

func (r *resumeFakeAPI) ListParts(ctx context.Context, in *s3.ListPartsInput, opts ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	return &s3.ListPartsOutput{Parts: r.existing, IsTruncated: aws.Bool(false)}, nil
}

func (r *resumeFakeAPI) UploadPart(ctx context.Context, in *s3.UploadPartInput, opts ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	n := aws.ToInt32(in.PartNumber)
	r.uploaded = append(r.uploaded, n)
	return &s3.UploadPartOutput{ETag: aws.String("\"new-etag\"")}, nil
}

func (r *resumeFakeAPI) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	if in.MultipartUpload != nil {
		r.completed = in.MultipartUpload.Parts
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

// TestResumeServerUpload resumes a 4-part object that already has parts 1-2 on
// the server: it must adopt the UploadId (no Create), upload only 3-4, and
// complete with the full ordered set.
func TestResumeServerUpload(t *testing.T) {
	withTempVerdaHome(t)
	abs, _, _ := writeTempFile(t, 3*minPartSize+100) // 4 parts at 5 MiB

	fake := &resumeFakeAPI{existing: []s3types.Part{
		{PartNumber: aws.Int32(1), Size: aws.Int64(minPartSize), ETag: aws.String("\"e1\"")},
		{PartNumber: aws.Int32(2), Size: aws.Int64(minPartSize), ETag: aws.String("\"e2\"")},
	}}
	f := cmdutil.NewTestFactory(nil)
	io := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	if err := resumeServerUpload(context.Background(), f, io, fake, "b", "cli-test/model.bin", "u1", abs); err != nil {
		t.Fatalf("resumeServerUpload: %v", err)
	}

	if fake.createCalls != 0 {
		t.Errorf("CreateMultipartUpload called %d times, want 0 (must adopt the existing UploadId)", fake.createCalls)
	}
	sort.Slice(fake.uploaded, func(i, j int) bool { return fake.uploaded[i] < fake.uploaded[j] })
	if len(fake.uploaded) != 2 || fake.uploaded[0] != 3 || fake.uploaded[1] != 4 {
		t.Errorf("uploaded parts = %v, want [3 4] (only the missing ones)", fake.uploaded)
	}
	if len(fake.completed) != 4 {
		t.Errorf("completed with %d parts, want 4", len(fake.completed))
	}
}
