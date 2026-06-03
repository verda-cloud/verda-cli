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
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// keysFromDeletes / keysFromCopies / keysFromUploads collect+sort keys for
// order-independent assertions in the recursive mv tests.
func keysFromDeletes(ins []*s3.DeleteObjectInput) []string {
	out := make([]string, 0, len(ins))
	for _, in := range ins {
		out = append(out, aws.ToString(in.Key))
	}
	sort.Strings(out)
	return out
}

func keysFromCopies(ins []*s3.CopyObjectInput) []string {
	out := make([]string, 0, len(ins))
	for _, in := range ins {
		out = append(out, aws.ToString(in.Key))
	}
	sort.Strings(out)
	return out
}

func keysFromUploads(ins []*s3.PutObjectInput) []string {
	out := make([]string, 0, len(ins))
	for _, in := range ins {
		out = append(out, aws.ToString(in.Key))
	}
	sort.Strings(out)
	return out
}

// TestMv_RecursiveUpload exercises uploadMoveTree: every file under the local
// dir is uploaded with its relative path preserved, then removed from disk.
func TestMv_RecursiveUpload(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "sub", "c.txt"), []byte("C"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	restore := withFakeClient(&mvFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/prefix/", "--recursive"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	gotKeys := keysFromUploads(fakeT.uploads)
	want := []string{"prefix/a.txt", "prefix/sub/c.txt"}
	if len(gotKeys) != len(want) {
		t.Fatalf("uploaded keys = %v, want %v", gotKeys, want)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			t.Errorf("uploaded keys[%d] = %q, want %q (all=%v)", i, gotKeys[i], want[i], gotKeys)
		}
	}
	// Both local sources must be gone after a successful move.
	for _, p := range []string{filepath.Join(tmp, "a.txt"), filepath.Join(tmp, "sub", "c.txt")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("source %s still present after recursive mv (err=%v)", p, err)
		}
	}
}

// TestMv_RecursiveDownload exercises downloadMoveTree: every listed object is
// downloaded under the dest dir with its relative path preserved, then deleted
// from the bucket.
func TestMv_RecursiveDownload(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("data/a.txt"), Size: aws.Int64(1)},
					{Key: aws.String("data/sub/b.txt"), Size: aws.Int64(1)},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()
	fakeT := &cpFakeTransporter{downloadWrite: []byte("X")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/data/", tmp, "--recursive"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeT.downloads) != 2 {
		t.Fatalf("Download calls = %d, want 2", len(fakeT.downloads))
	}
	for _, p := range []string{filepath.Join(tmp, "a.txt"), filepath.Join(tmp, "sub", "b.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected downloaded file at %s: %v", p, err)
		}
	}
	// Each source object is deleted after its download succeeds.
	gotDel := keysFromDeletes(fakeAPI.deleteInputs)
	want := []string{"data/a.txt", "data/sub/b.txt"}
	if len(gotDel) != len(want) {
		t.Fatalf("deleted source keys = %v, want %v", gotDel, want)
	}
	for i := range want {
		if gotDel[i] != want[i] {
			t.Errorf("deleted keys[%d] = %q, want %q (all=%v)", i, gotDel[i], want[i], gotDel)
		}
	}
}

// TestMv_RecursiveS3ToS3 exercises s3MoveTree: copy each key to the dest prefix
// then delete the source key.
func TestMv_RecursiveS3ToS3(t *testing.T) {
	// no t.Parallel
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("src/a.txt"), Size: aws.Int64(1)},
					{Key: aws.String("src/b.txt"), Size: aws.Int64(1)},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://src-bucket/src/", "s3://dst-bucket/dst/", "--recursive"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	gotCopy := keysFromCopies(fakeAPI.copyInputs)
	wantCopy := []string{"dst/a.txt", "dst/b.txt"}
	if len(gotCopy) != len(wantCopy) {
		t.Fatalf("copied keys = %v, want %v", gotCopy, wantCopy)
	}
	for i := range wantCopy {
		if gotCopy[i] != wantCopy[i] {
			t.Errorf("copied keys[%d] = %q, want %q (all=%v)", i, gotCopy[i], wantCopy[i], gotCopy)
		}
	}
	gotDel := keysFromDeletes(fakeAPI.deleteInputs)
	wantDel := []string{"src/a.txt", "src/b.txt"}
	if len(gotDel) != len(wantDel) {
		t.Fatalf("deleted source keys = %v, want %v", gotDel, wantDel)
	}
	for i := range wantDel {
		if gotDel[i] != wantDel[i] {
			t.Errorf("deleted keys[%d] = %q, want %q (all=%v)", i, gotDel[i], wantDel[i], gotDel)
		}
	}
}

// mvFakeAPI extends cpFakeAPI with DeleteObject recording so mv tests can
// assert the post-transfer source cleanup.
type mvFakeAPI struct {
	cpFakeAPI
	deleteInputs []*s3.DeleteObjectInput
	deleteErr    error
}

func (c *mvFakeAPI) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	c.deleteInputs = append(c.deleteInputs, in)
	return &s3.DeleteObjectOutput{}, nil
}

func TestMv_LocalToLocal_Error(t *testing.T) {
	// no t.Parallel — clientBuilder/transporterBuilder mutation
	restore := withFakeClient(&mvFakeAPI{})
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"./a", "./b"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when both sides are local")
	}
	if !strings.Contains(err.Error(), "s3://") {
		t.Errorf("error should mention s3://, got: %v", err)
	}
}

func TestMv_Upload_SingleFile(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	src := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(src, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	restore := withFakeClient(&mvFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/hello.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeT.uploads) != 1 {
		t.Fatalf("Upload calls = %d, want 1", len(fakeT.uploads))
	}
	got := fakeT.uploads[0]
	if aws.ToString(got.Bucket) != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", aws.ToString(got.Bucket))
	}
	if aws.ToString(got.Key) != "dest/hello.txt" {
		t.Errorf("Key = %q, want dest/hello.txt", aws.ToString(got.Key))
	}
	// Source must have been removed after a successful upload.
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source file still exists after mv: Stat err = %v", err)
	}
	if !strings.Contains(out.String(), "moved") {
		t.Errorf("stdout missing 'moved':\n%s", out.String())
	}
}

func TestMv_Download_SingleFile(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out.txt")

	fakeT := &cpFakeTransporter{downloadWrite: []byte("hello")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	fakeAPI := &mvFakeAPI{}
	restore := withFakeClient(fakeAPI)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/path/file.txt", dst})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeT.downloads) != 1 {
		t.Fatalf("Download calls = %d, want 1", len(fakeT.downloads))
	}
	body, err := os.ReadFile(dst) // #nosec G304 -- dst is under t.TempDir()
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("local file = %q, want %q", string(body), "hello")
	}
	if len(fakeAPI.deleteInputs) != 1 {
		t.Fatalf("DeleteObject calls = %d, want 1", len(fakeAPI.deleteInputs))
	}
	del := fakeAPI.deleteInputs[0]
	if aws.ToString(del.Bucket) != "my-bucket" {
		t.Errorf("delete bucket = %q, want my-bucket", aws.ToString(del.Bucket))
	}
	if aws.ToString(del.Key) != "path/file.txt" {
		t.Errorf("delete key = %q, want path/file.txt", aws.ToString(del.Key))
	}
	if !strings.Contains(out.String(), "moved") {
		t.Errorf("stdout missing 'moved':\n%s", out.String())
	}
}

func TestMv_S3ToS3_SingleFile(t *testing.T) {
	// no t.Parallel
	fakeAPI := &mvFakeAPI{}
	restore := withFakeClient(fakeAPI)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	// Use a key with a space to verify per-component URL encoding of
	// CopySource — bucket/key are encoded separately with a literal '/'
	// between them, so the result must be "src-bucket/hello%20world.txt"
	// (NOT "src-bucket%2Fhello%20world.txt").
	cmd.SetArgs([]string{"s3://src-bucket/hello world.txt", "s3://dst-bucket/b.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeAPI.copyInputs) != 1 {
		t.Fatalf("CopyObject calls = %d, want 1", len(fakeAPI.copyInputs))
	}
	got := fakeAPI.copyInputs[0]
	if aws.ToString(got.Bucket) != "dst-bucket" {
		t.Errorf("Bucket = %q, want dst-bucket", aws.ToString(got.Bucket))
	}
	if aws.ToString(got.Key) != "b.txt" {
		t.Errorf("Key = %q, want b.txt", aws.ToString(got.Key))
	}
	cs := aws.ToString(got.CopySource)
	if cs != "src-bucket/hello%20world.txt" {
		t.Errorf("CopySource = %q, want %q", cs, "src-bucket/hello%20world.txt")
	}

	// Source object must have been deleted after the copy succeeded.
	if len(fakeAPI.deleteInputs) != 1 {
		t.Fatalf("DeleteObject calls = %d, want 1", len(fakeAPI.deleteInputs))
	}
	del := fakeAPI.deleteInputs[0]
	if aws.ToString(del.Bucket) != "src-bucket" {
		t.Errorf("delete bucket = %q, want src-bucket", aws.ToString(del.Bucket))
	}
	if aws.ToString(del.Key) != "hello world.txt" {
		t.Errorf("delete key = %q, want %q", aws.ToString(del.Key), "hello world.txt")
	}
	if !strings.Contains(out.String(), "moved") {
		t.Errorf("stdout missing 'moved':\n%s", out.String())
	}
}

func TestMv_Dryrun(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	src := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	fakeAPI := &mvFakeAPI{}
	restore := withFakeClient(fakeAPI)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dst/hello.txt", "--dryrun"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeT.uploads) != 0 {
		t.Errorf("Upload should not be called in dryrun, got %d", len(fakeT.uploads))
	}
	if len(fakeT.downloads) != 0 {
		t.Errorf("Download should not be called in dryrun, got %d", len(fakeT.downloads))
	}
	if len(fakeAPI.copyInputs) != 0 {
		t.Errorf("CopyObject should not be called in dryrun, got %d", len(fakeAPI.copyInputs))
	}
	if len(fakeAPI.deleteInputs) != 0 {
		t.Errorf("DeleteObject should not be called in dryrun, got %d", len(fakeAPI.deleteInputs))
	}
	// Source must still exist — dryrun issues no side effects.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file removed during dryrun: %v", err)
	}
	if !strings.Contains(out.String(), "(dry run) would move") {
		t.Errorf("stdout missing '(dry run) would move':\n%s", out.String())
	}
}

func TestMv_Upload_TransferFails_NoDelete(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	src := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(src, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	fakeT := &cpFakeTransporter{uploadErr: errors.New("boom")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	restore := withFakeClient(&mvFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdMv(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/hello.txt"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected Execute to fail when Upload errors")
	}
	// Source must still exist because the transfer failed — delete is skipped.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source file removed despite failed upload: %v", err)
	}
}
