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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

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
