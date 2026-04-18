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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// writeFileWithMtime writes the contents to path and then sets both access
// and modification times to mt so tests can precisely control timestamps.
func writeFileWithMtime(t *testing.T, path string, body []byte, mt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// sortedUploadKeys returns the bucket keys of every recorded upload, sorted.
func sortedUploadKeys(up []*s3.PutObjectInput) []string {
	out := make([]string, 0, len(up))
	for _, u := range up {
		out = append(out, aws.ToString(u.Key))
	}
	sort.Strings(out)
	return out
}

// sortedDownloadKeys returns the source keys of every recorded download, sorted.
func sortedDownloadKeys(dl []*s3.GetObjectInput) []string {
	out := make([]string, 0, len(dl))
	for _, d := range dl {
		out = append(out, aws.ToString(d.Key))
	}
	sort.Strings(out)
	return out
}

func TestSync_LocalToS3_CopiesNewAndUpdated(t *testing.T) {
	// no t.Parallel — clientBuilder/transporterBuilder mutation
	tmp := t.TempDir()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	// Local files: a.txt (matches remote), b.txt (older/smaller remote), c.txt (new, no remote).
	writeFileWithMtime(t, filepath.Join(tmp, "a.txt"), []byte("AAAA"), now)
	writeFileWithMtime(t, filepath.Join(tmp, "b.txt"), []byte("BBBBBB"), now)
	writeFileWithMtime(t, filepath.Join(tmp, "c.txt"), []byte("CCC"), now)

	// Remote: a matches (same size + same-or-newer mtime), b has smaller size
	// (triggers re-upload), c is absent (triggers upload).
	older := now.Add(-time.Hour)
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("prefix/a.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
				{Key: aws.String("prefix/b.txt"), Size: aws.Int64(1), LastModified: aws.Time(older)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()

	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/prefix/"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Expect 2 uploads: b.txt (size mismatch) and c.txt (missing). a.txt
	// should be skipped (size match + not-newer).
	gotKeys := sortedUploadKeys(fakeT.uploads)
	wantKeys := []string{"prefix/b.txt", "prefix/c.txt"}
	if !equalStrings(gotKeys, wantKeys) {
		t.Fatalf("uploaded keys = %v, want %v", gotKeys, wantKeys)
	}
	// No deletes without --delete.
	if len(fakeAPI.deleteInputs) != 0 {
		t.Errorf("DeleteObject called %d times, want 0", len(fakeAPI.deleteInputs))
	}
	if !strings.Contains(out.String(), "copied") {
		t.Errorf("stdout missing 'copied':\n%s", out.String())
	}
}

func TestSync_S3ToLocal_Download(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()

	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	// Local already has a.txt matching size AND not-older mtime.
	writeFileWithMtime(t, filepath.Join(tmp, "a.txt"), []byte("AAAA"), now)

	// Remote has a.txt (matches local) and b.txt (missing locally).
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("data/a.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
				{Key: aws.String("data/b.txt"), Size: aws.Int64(3), LastModified: aws.Time(now)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()

	fakeT := &cpFakeTransporter{downloadWrite: []byte("BBB")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket/data/", tmp})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	gotKeys := sortedDownloadKeys(fakeT.downloads)
	wantKeys := []string{"data/b.txt"}
	if !equalStrings(gotKeys, wantKeys) {
		t.Fatalf("downloaded keys = %v, want %v", gotKeys, wantKeys)
	}
	// b.txt now exists locally.
	if _, err := os.Stat(filepath.Join(tmp, "b.txt")); err != nil {
		t.Errorf("expected b.txt on disk: %v", err)
	}
}

func TestSync_S3ToS3_WithDelete(t *testing.T) {
	// no t.Parallel
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	// listDispatchAPI serves different pages depending on the bucket requested.
	fake := &syncDualAPI{
		pagesByBucket: map[string]*s3.ListObjectsV2Output{
			"src-bucket": {
				Contents: []s3types.Object{
					{Key: aws.String("a"), Size: aws.Int64(1), LastModified: aws.Time(now)},
					{Key: aws.String("b"), Size: aws.Int64(1), LastModified: aws.Time(now)},
				},
				IsTruncated: aws.Bool(false),
			},
			"dst-bucket": {
				Contents: []s3types.Object{
					{Key: aws.String("a"), Size: aws.Int64(1), LastModified: aws.Time(now)},
					{Key: aws.String("c"), Size: aws.Int64(1), LastModified: aws.Time(now)},
				},
				IsTruncated: aws.Bool(false),
			},
		},
	}
	restore := withFakeClient(fake)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://src-bucket/", "s3://dst-bucket/", "--delete"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Expect CopyObject for "b" (missing in dst) only.
	if len(fake.copyInputs) != 1 {
		t.Fatalf("CopyObject calls = %d, want 1", len(fake.copyInputs))
	}
	if got := aws.ToString(fake.copyInputs[0].Key); got != "b" {
		t.Errorf("copy key = %q, want %q", got, "b")
	}
	// Expect DeleteObject for "c" (present in dst, absent in src).
	if len(fake.deleteInputs) != 1 {
		t.Fatalf("DeleteObject calls = %d, want 1", len(fake.deleteInputs))
	}
	if got := aws.ToString(fake.deleteInputs[0].Key); got != "c" {
		t.Errorf("delete key = %q, want %q", got, "c")
	}
	if got := aws.ToString(fake.deleteInputs[0].Bucket); got != "dst-bucket" {
		t.Errorf("delete bucket = %q, want %q", got, "dst-bucket")
	}
}

func TestSync_ExactTimestamps(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	// Local "a": same size as remote but local mtime is 1 ms NEWER.
	writeFileWithMtime(t, filepath.Join(tmp, "a.txt"), []byte("SAME"), now.Add(time.Millisecond))
	// Local "b": same size AND exactly the same mtime → should be skipped
	// under either semantics.
	writeFileWithMtime(t, filepath.Join(tmp, "b.txt"), []byte("SAME"), now)

	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("a.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
				{Key: aws.String("b.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}

	// --- Run 1: without --exact-timestamps (default: "newer OR size differs")
	restore := withFakeClient(fakeAPI)
	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/"})
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute (default): %v", err)
	}
	// a.txt is newer → uploaded. b.txt is exactly-equal → skipped.
	if got := sortedUploadKeys(fakeT.uploads); !equalStrings(got, []string{"a.txt"}) {
		t.Fatalf("default uploads = %v, want [a.txt]", got)
	}
	restore()
	restoreT()

	// --- Run 2: with --exact-timestamps (copy if timestamps differ AT ALL)
	fakeAPI2 := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("a.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
				{Key: aws.String("b.txt"), Size: aws.Int64(4), LastModified: aws.Time(now)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}
	restore2 := withFakeClient(fakeAPI2)
	defer restore2()
	fakeT2 := &cpFakeTransporter{}
	restoreT2 := withFakeTransporter(fakeT2)
	defer restoreT2()

	out2 := &bytes.Buffer{}
	cmd2 := NewCmdSync(f, cmdutil.IOStreams{Out: out2, ErrOut: &bytes.Buffer{}})
	cmd2.SetArgs([]string{tmp, "s3://my-bucket/", "--exact-timestamps"})
	cmd2.SetContext(context.Background())
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("Execute (exact-timestamps): %v", err)
	}
	// a.txt still uploaded (timestamps differ), b.txt still skipped (equal).
	if got := sortedUploadKeys(fakeT2.uploads); !equalStrings(got, []string{"a.txt"}) {
		t.Fatalf("exact-timestamps uploads = %v, want [a.txt]", got)
	}
}

func TestSync_Dryrun_NoSideEffects(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	writeFileWithMtime(t, filepath.Join(tmp, "new.txt"), []byte("NEW"), now)

	// Remote has one "stale" object that a --delete would target.
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("stale.txt"), Size: aws.Int64(5), LastModified: aws.Time(now)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()
	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/", "--delete", "--dryrun"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// No side-effects of any kind.
	if len(fakeT.uploads) != 0 {
		t.Errorf("Upload calls = %d, want 0", len(fakeT.uploads))
	}
	if len(fakeT.downloads) != 0 {
		t.Errorf("Download calls = %d, want 0", len(fakeT.downloads))
	}
	if len(fakeAPI.copyInputs) != 0 {
		t.Errorf("CopyObject calls = %d, want 0", len(fakeAPI.copyInputs))
	}
	if len(fakeAPI.deleteInputs) != 0 {
		t.Errorf("DeleteObject calls = %d, want 0", len(fakeAPI.deleteInputs))
	}
	if !strings.Contains(out.String(), "(dry run)") {
		t.Errorf("stdout missing '(dry run)' preview:\n%s", out.String())
	}
}

func TestSync_Filters(t *testing.T) {
	// no t.Parallel
	tmp := t.TempDir()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	// Two new files locally, both missing remotely.
	writeFileWithMtime(t, filepath.Join(tmp, "keep.txt"), []byte("TXT"), now)
	writeFileWithMtime(t, filepath.Join(tmp, "skip.log"), []byte("LOG"), now)

	// Remote has a .log that --delete would normally remove — but the
	// filter must exclude it too.
	fakeAPI := &mvFakeAPI{cpFakeAPI: cpFakeAPI{
		listObjectsPages: []*s3.ListObjectsV2Output{{
			Contents: []s3types.Object{
				{Key: aws.String("old.log"), Size: aws.Int64(5), LastModified: aws.Time(now)},
			},
			IsTruncated: aws.Bool(false),
		}},
	}}
	restore := withFakeClient(fakeAPI)
	defer restore()
	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{tmp, "s3://my-bucket/", "--include", "*.txt", "--exclude", "*.log", "--delete"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Only keep.txt is uploaded.
	if got := sortedUploadKeys(fakeT.uploads); !equalStrings(got, []string{"keep.txt"}) {
		t.Fatalf("uploads = %v, want [keep.txt]", got)
	}
	// old.log matches --exclude, so it must NOT be deleted despite --delete.
	if len(fakeAPI.deleteInputs) != 0 {
		t.Errorf("DeleteObject calls = %d, want 0 (old.log excluded by filter)", len(fakeAPI.deleteInputs))
	}
}

func TestSync_LocalToLocal_Error(t *testing.T) {
	// no t.Parallel — clientBuilder/transporterBuilder mutation
	restore := withFakeClient(&mvFakeAPI{})
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdSync(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
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

// ---- test helpers --------------------------------------------------------

// syncDualAPI serves per-bucket ListObjectsV2 pages so S3-to-S3 sync tests can
// mock both sides with a single fake. copy + delete calls are recorded.
type syncDualAPI struct {
	API
	pagesByBucket map[string]*s3.ListObjectsV2Output
	copyInputs    []*s3.CopyObjectInput
	deleteInputs  []*s3.DeleteObjectInput
}

func (s *syncDualAPI) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	page, ok := s.pagesByBucket[aws.ToString(in.Bucket)]
	if !ok {
		return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}, nil
	}
	return page, nil
}

func (s *syncDualAPI) CopyObject(ctx context.Context, in *s3.CopyObjectInput, opts ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	s.copyInputs = append(s.copyInputs, in)
	return &s3.CopyObjectOutput{}, nil
}

func (s *syncDualAPI) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	s.deleteInputs = append(s.deleteInputs, in)
	return &s3.DeleteObjectOutput{}, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
