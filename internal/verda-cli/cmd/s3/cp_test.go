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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// cpFakeTransporter records Upload / Download invocations for the cp tests.
type cpFakeTransporter struct {
	uploads       []*s3.PutObjectInput
	uploadBodies  [][]byte
	downloads     []*s3.GetObjectInput
	downloadWrite []byte // bytes written to each WriterAt during Download
	uploadErr     error
	downloadErr   error
}

func (t *cpFakeTransporter) Upload(ctx context.Context, in *s3.PutObjectInput) (*manager.UploadOutput, error) {
	if t.uploadErr != nil {
		return nil, t.uploadErr
	}
	t.uploads = append(t.uploads, in)
	// Drain body for realism + byte accounting.
	var body []byte
	if in.Body != nil {
		b, err := io.ReadAll(in.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	t.uploadBodies = append(t.uploadBodies, body)
	return &manager.UploadOutput{Location: "s3://" + aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)}, nil
}

func (t *cpFakeTransporter) Download(ctx context.Context, w io.WriterAt, in *s3.GetObjectInput) (int64, error) {
	if t.downloadErr != nil {
		return 0, t.downloadErr
	}
	t.downloads = append(t.downloads, in)
	if len(t.downloadWrite) > 0 {
		n, err := w.WriteAt(t.downloadWrite, 0)
		if err != nil {
			return int64(n), err
		}
		return int64(n), nil
	}
	return 0, nil
}

// cpFakeAPI is a minimal API implementation for the cp command tests.
// It records CopyObject calls and serves paginated ListObjectsV2 pages.
type cpFakeAPI struct {
	API
	copyInputs       []*s3.CopyObjectInput
	copyErr          error
	listObjectsPages []*s3.ListObjectsV2Output
	listObjectsCalls int
	listErr          error
}

func (c *cpFakeAPI) CopyObject(ctx context.Context, in *s3.CopyObjectInput, opts ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	if c.copyErr != nil {
		return nil, c.copyErr
	}
	c.copyInputs = append(c.copyInputs, in)
	return &s3.CopyObjectOutput{}, nil
}

func (c *cpFakeAPI) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	if c.listObjectsCalls >= len(c.listObjectsPages) {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
	}
	page := c.listObjectsPages[c.listObjectsCalls]
	c.listObjectsCalls++
	return page, nil
}

// withFakeTransporter swaps transporterBuilder for tests.
func withFakeTransporter(fake Transporter) (restore func()) {
	orig := transporterBuilder
	transporterBuilder = func(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (Transporter, error) {
		return fake, nil
	}
	return func() { transporterBuilder = orig }
}

func TestCp_LocalToLocal_Error(t *testing.T) {
	// no t.Parallel — clientBuilder/transporterBuilder mutation
	restore := withFakeClient(&cpFakeAPI{})
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
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

func TestCp_Upload_SingleFile(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	src := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(src, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	fake := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fake)
	defer restoreT()
	restore := withFakeClient(&cpFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/hello.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.uploads) != 1 {
		t.Fatalf("Upload calls = %d, want 1", len(fake.uploads))
	}
	got := fake.uploads[0]
	if aws.ToString(got.Bucket) != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", aws.ToString(got.Bucket))
	}
	if aws.ToString(got.Key) != "dest/hello.txt" {
		t.Errorf("Key = %q, want dest/hello.txt", aws.ToString(got.Key))
	}
	if ct := aws.ToString(got.ContentType); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("ContentType = %q, want text/plain*", ct)
	}
	if !strings.Contains(out.String(), "uploaded") {
		t.Errorf("stdout missing 'uploaded':\n%s", out.String())
	}
}

func TestCp_Download_SingleFile(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out.txt")

	fake := &cpFakeTransporter{downloadWrite: []byte("hello")}
	restoreT := withFakeTransporter(fake)
	defer restoreT()
	restore := withFakeClient(&cpFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/path/file.txt", dst})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.downloads) != 1 {
		t.Fatalf("Download calls = %d, want 1", len(fake.downloads))
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("local file = %q, want %q", string(body), "hello")
	}
	if !strings.Contains(out.String(), "downloaded") {
		t.Errorf("stdout missing 'downloaded':\n%s", out.String())
	}
}

func TestCp_S3ToS3_SingleFile(t *testing.T) {
	// no t.Parallel
	fake := &cpFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	// Use a key with a space to verify per-component URL encoding of
	// CopySource — bucket/key are encoded separately with a literal '/'
	// between them, so the result must be "src-bucket/hello%20world.txt"
	// (NOT "src-bucket%2Fhello%20world.txt").
	cmd.SetArgs([]string{"s3://src-bucket/hello world.txt", "s3://dst-bucket/b.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.copyInputs) != 1 {
		t.Fatalf("CopyObject calls = %d, want 1", len(fake.copyInputs))
	}
	got := fake.copyInputs[0]
	if aws.ToString(got.Bucket) != "dst-bucket" {
		t.Errorf("Bucket = %q, want dst-bucket", aws.ToString(got.Bucket))
	}
	if aws.ToString(got.Key) != "b.txt" {
		t.Errorf("Key = %q, want b.txt", aws.ToString(got.Key))
	}
	// Exact match: bucket/key are percent-encoded individually, with a
	// literal '/' separating them. A space encodes as %20; the separator
	// must NOT encode as %2F.
	cs := aws.ToString(got.CopySource)
	if cs != "src-bucket/hello%20world.txt" {
		t.Errorf("CopySource = %q, want %q", cs, "src-bucket/hello%20world.txt")
	}
	if !strings.Contains(out.String(), "copied") {
		t.Errorf("stdout missing 'copied':\n%s", out.String())
	}
}

func TestCp_RecursiveUpload_WithInclude(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.log"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "subdir", "c.txt"), []byte("C"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fake)
	defer restoreT()
	restore := withFakeClient(&cpFakeAPI{})
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/prefix/", "--recursive", "--include", "*.txt"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// With filepath.Match-style '*', '*.txt' matches ONLY "a.txt" at the root.
	// "subdir/c.txt" is NOT matched because '*' does not cross '/'.
	if len(fake.uploads) != 1 {
		keys := make([]string, 0, len(fake.uploads))
		for _, u := range fake.uploads {
			keys = append(keys, aws.ToString(u.Key))
		}
		t.Fatalf("Upload calls = %d (keys=%v), want 1 (a.txt only)", len(fake.uploads), keys)
	}
	if k := aws.ToString(fake.uploads[0].Key); k != "prefix/a.txt" {
		t.Errorf("key = %q, want prefix/a.txt", k)
	}
}

func TestCp_RecursiveDownload(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()

	fakeAPI := &cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					{Key: aws.String("data/a.txt"), Size: aws.Int64(1)},
					{Key: aws.String("data/sub/b.txt"), Size: aws.Int64(1)},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fakeAPI)
	defer restore()

	fakeT := &cpFakeTransporter{downloadWrite: []byte("X")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/data/", tmp, "--recursive"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fakeT.downloads) != 2 {
		t.Fatalf("Download calls = %d, want 2", len(fakeT.downloads))
	}
	// Verify that both files were written at the expected relative paths.
	wantFiles := []string{
		filepath.Join(tmp, "a.txt"),
		filepath.Join(tmp, "sub", "b.txt"),
	}
	sort.Strings(wantFiles)
	for _, p := range wantFiles {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected downloaded file at %s: %v", p, err)
		}
	}
}

func TestCp_Dryrun(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	src := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()
	fakeAPI := &cpFakeAPI{}
	restore := withFakeClient(fakeAPI)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
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
	if !strings.Contains(out.String(), "(dry run) would") {
		t.Errorf("stdout missing dry run preview:\n%s", out.String())
	}
}

// TestCp_RecursiveDownload_EscapeAttempt verifies that an adversarial S3 key
// containing ".." segments cannot cause a local write outside the declared
// destination directory. The command must return an error and leave no
// files on disk outside dstDir.
func TestCp_RecursiveDownload_EscapeAttempt(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	dstDir := filepath.Join(tmp, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	fakeAPI := &cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{
			{
				Contents: []s3types.Object{
					// Adversarial key: would resolve outside dstDir if
					// filepath.Join cleans the ".." segments.
					{Key: aws.String("../../etc/passwd"), Size: aws.Int64(1)},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fakeAPI)
	defer restore()

	fakeT := &cpFakeTransporter{downloadWrite: []byte("pwned")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://evil-bucket/", dstDir, "--recursive"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for path-traversal key, got nil")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error = %v, want mention of 'escape'", err)
	}
	// No download should have been attempted.
	if len(fakeT.downloads) != 0 {
		t.Errorf("Download called %d times, want 0 (must abort before any write)", len(fakeT.downloads))
	}
	// And no local file should exist at the escape target.
	if _, statErr := os.Stat(filepath.Join(tmp, "etc", "passwd")); statErr == nil {
		t.Errorf("file written outside dstDir: %s", filepath.Join(tmp, "etc", "passwd"))
	}
}

func TestCp_InvalidURI(t *testing.T) {
	// no t.Parallel
	restore := withFakeClient(&cpFakeAPI{})
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://", "./local"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for malformed s3:// URI")
	}
}
