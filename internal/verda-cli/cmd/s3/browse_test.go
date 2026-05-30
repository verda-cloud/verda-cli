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
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// browseFakeAPI is prefix-aware: the root level exposes a "data/" folder, and
// the "data/" level exposes a single object. It records DeleteObject calls.
type browseFakeAPI struct {
	API
	deleteInputs []*s3.DeleteObjectInput
}

func (b *browseFakeAPI) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{Buckets: []s3types.Bucket{{Name: aws.String("b")}}}, nil
}

func (b *browseFakeAPI) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	out := &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}
	switch aws.ToString(in.Prefix) {
	case "":
		out.CommonPrefixes = []s3types.CommonPrefix{{Prefix: aws.String("data/")}}
	case "data/":
		out.Contents = []s3types.Object{{Key: aws.String("data/file.txt"), Size: aws.Int64(1024)}}
	}
	return out, nil
}

func (b *browseFakeAPI) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	b.deleteInputs = append(b.deleteInputs, in)
	return &s3.DeleteObjectOutput{}, nil
}

// TestBrowse_DrillDownDeleteAndExit walks bucket -> data/ folder -> file.txt,
// deletes it via the action menu, then exits — exercising browseBuckets,
// browseLevel, buildBrowseRows, objectActionMenu and browseDelete.
func TestBrowse_DrillDownDeleteAndExit(t *testing.T) {
	// no t.Parallel — prompter/clientBuilder state
	fake := &browseFakeAPI{}

	// Select sequence:
	//   0 -> bucket "b"
	//   1 -> folder "data/"   (root rows: up, 📁data/, exit — no objects, no multi row)
	//   2 -> object file.txt  (data/ rows: up, ⬇download-multi, 📄file.txt, exit)
	//   2 -> Delete           (menu: Download, Info, Delete, Back)
	//   3 -> Exit             (post-delete re-list: up, ⬇download-multi, 📄file.txt, exit)
	mock := tuitest.New().
		AddSelect(0).AddSelect(1).AddSelect(2).AddSelect(2).AddSelect(3).
		AddConfirm(true)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)

	if err := runLsBrowser(context.Background(), f, cmdutil.IOStreams{Out: out, ErrOut: errOut}, fake); err != nil {
		t.Fatalf("runLsBrowser: %v", err)
	}

	if len(fake.deleteInputs) != 1 {
		t.Fatalf("DeleteObject calls = %d, want 1", len(fake.deleteInputs))
	}
	if k := aws.ToString(fake.deleteInputs[0].Key); k != "data/file.txt" {
		t.Errorf("deleted key = %q, want data/file.txt", k)
	}
	if !strings.Contains(out.String(), "deleted") {
		t.Errorf("stdout missing delete confirmation:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "permanently delete") {
		t.Errorf("stderr missing destructive warning:\n%s", errOut.String())
	}
}

// TestBrowse_MultiDownload drills into data/, opens the multi-download entry,
// ticks the one object, downloads it, then exits.
func TestBrowse_MultiDownload(t *testing.T) {
	// no t.Parallel — prompter/transporter/cwd state
	t.Chdir(t.TempDir()) // isolate the cwd that downloads write into

	fake := &browseFakeAPI{}
	fakeT := &cpFakeTransporter{downloadWrite: []byte("XYZ")}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	// Selects: bucket(0) -> folder data/(1) -> Download-files-here(1) -> Exit(3)
	// MultiSelect: tick the single object [0].
	mock := tuitest.New().
		AddSelect(0).AddSelect(1).AddSelect(1).AddSelect(3).
		AddMultiSelect([]int{0})

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)

	if err := runLsBrowser(context.Background(), f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("runLsBrowser: %v", err)
	}

	if len(fakeT.downloads) != 1 {
		t.Fatalf("Download calls = %d, want 1", len(fakeT.downloads))
	}
	if k := aws.ToString(fakeT.downloads[0].Key); k != "data/file.txt" {
		t.Errorf("downloaded key = %q, want data/file.txt", k)
	}
	if !strings.Contains(out.String(), "Downloaded 1 file(s)") {
		t.Errorf("stdout missing multi-download summary:\n%s", out.String())
	}
}

func TestAscend(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"data/sub/", "data/"},
		{"data/", ""},
		{"data/file.txt", "data/"},
		{"", ""}, // bucket cleared separately; key stays ""
	}
	for _, tc := range cases {
		cur := URI{Bucket: "b", Key: tc.in}
		ascend(&cur)
		if tc.in == "" {
			if cur.Bucket != "" {
				t.Errorf("ascend at key root should clear bucket, got %q", cur.Bucket)
			}
			continue
		}
		if cur.Key != tc.want {
			t.Errorf("ascend(%q) key = %q, want %q", tc.in, cur.Key, tc.want)
		}
	}
}
