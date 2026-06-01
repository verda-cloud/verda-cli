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
	"github.com/verda-cloud/verdagostack/pkg/tui"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// ----- mb: interactive name prompt ---------------------------------------

func TestPromptNewBucketName_TrimsInput(t *testing.T) {
	// no t.Parallel — prompter state
	f := cmdutil.NewTestFactory(tuitest.New().AddTextInput("  my-bucket  "))
	got, err := promptNewBucketName(context.Background(), f)
	if err != nil {
		t.Fatalf("promptNewBucketName: %v", err)
	}
	if got != "my-bucket" {
		t.Errorf("name = %q, want my-bucket (trimmed)", got)
	}
}

func TestPromptNewBucketName_EmptyIsCleanCancel(t *testing.T) {
	f := cmdutil.NewTestFactory(tuitest.New().AddTextInput("   "))
	got, err := promptNewBucketName(context.Background(), f)
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want empty/no-error for blank input", got, err)
	}
}

// ----- picker: flat object selection (mv source) -------------------------

func TestSelectObjectKey_PicksChosen(t *testing.T) {
	fake := &fakeS3API{objects: []s3types.Object{
		{Key: aws.String("a.txt")},
		{Key: aws.String("b.txt")},
	}}
	f := cmdutil.NewTestFactory(tuitest.New().AddSelect(1))
	got, err := selectObjectKey(context.Background(), f, ioBufs(), fake, "bucket")
	if err != nil {
		t.Fatalf("selectObjectKey: %v", err)
	}
	if got != "b.txt" {
		t.Errorf("chosen key = %q, want b.txt", got)
	}
}

func TestSelectObjectKey_EmptyBucketReturnsBlank(t *testing.T) {
	fake := &fakeS3API{}
	f := cmdutil.NewTestFactory(tuitest.New())
	errOut := &bytes.Buffer{}
	got, err := selectObjectKey(context.Background(), f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: errOut}, fake, "bucket")
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want empty for empty bucket", got, err)
	}
	if !strings.Contains(errOut.String(), "No objects") {
		t.Errorf("missing empty-bucket note: %q", errOut.String())
	}
}

// ----- rm: interactive folder browser delete -----------------------------

// rmBrowseFake is prefix-aware (root exposes data/, data/ exposes one object)
// and records DeleteObjects keys, dropping deleted keys from later listings.
type rmBrowseFake struct {
	API
	deleted map[string]bool
}

func (r *rmBrowseFake) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	return &s3.ListBucketsOutput{Buckets: []s3types.Bucket{{Name: aws.String("b")}}}, nil
}

func (r *rmBrowseFake) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(in.Prefix)
	out := &s3.ListObjectsV2Output{IsTruncated: aws.Bool(false)}
	switch prefix {
	case "":
		out.CommonPrefixes = []s3types.CommonPrefix{{Prefix: aws.String("data/")}}
	case "data/":
		if !r.deleted["data/file.txt"] {
			out.Contents = []s3types.Object{{Key: aws.String("data/file.txt"), Size: aws.Int64(10)}}
		}
	}
	return out, nil
}

func (r *rmBrowseFake) DeleteObjects(ctx context.Context, in *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	for i := range in.Delete.Objects {
		r.deleted[aws.ToString(in.Delete.Objects[i].Key)] = true
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func TestRmBrowser_DrillInMultiDelete(t *testing.T) {
	// no t.Parallel — prompter state
	fake := &rmBrowseFake{deleted: map[string]bool{}}

	// bucket b(0) -> folder data/(1) -> Delete-files-here(1) -> [tick 0] ->
	// confirm(true) -> re-list data/ (now empty: up, exit) -> Exit(1).
	mock := tuitest.New().
		AddSelect(0).AddSelect(1).AddSelect(1).
		AddMultiSelect([]int{0}).
		AddConfirm(true).
		AddSelect(1)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)

	if err := runRmBrowser(context.Background(), f, cmdutil.IOStreams{Out: out, ErrOut: errOut}, fake); err != nil {
		t.Fatalf("runRmBrowser: %v", err)
	}
	if !fake.deleted["data/file.txt"] {
		t.Errorf("data/file.txt was not deleted; deleted=%v", fake.deleted)
	}
	if !strings.Contains(out.String(), "deleted") {
		t.Errorf("missing delete confirmation on stdout:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "permanently delete") {
		t.Errorf("missing destructive warning on stderr:\n%s", errOut.String())
	}
}

// ----- mv: interactive S3->S3 move wizard --------------------------------

type mvWizardFake struct {
	API
	buckets   []string
	objects   []string
	copiedSrc string
	copiedDst string
	deleted   string
}

func (m *mvWizardFake) ListBuckets(ctx context.Context, in *s3.ListBucketsInput, opts ...func(*s3.Options)) (*s3.ListBucketsOutput, error) {
	bs := make([]s3types.Bucket, 0, len(m.buckets))
	for _, b := range m.buckets {
		bs = append(bs, s3types.Bucket{Name: aws.String(b)})
	}
	return &s3.ListBucketsOutput{Buckets: bs}, nil
}

func (m *mvWizardFake) ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	objs := make([]s3types.Object, 0, len(m.objects))
	for _, k := range m.objects {
		objs = append(objs, s3types.Object{Key: aws.String(k)})
	}
	return &s3.ListObjectsV2Output{Contents: objs, IsTruncated: aws.Bool(false)}, nil
}

func (m *mvWizardFake) CopyObject(ctx context.Context, in *s3.CopyObjectInput, opts ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	m.copiedSrc = aws.ToString(in.CopySource)
	m.copiedDst = aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)
	return &s3.CopyObjectOutput{}, nil
}

func (m *mvWizardFake) DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.deleted = aws.ToString(in.Bucket) + "/" + aws.ToString(in.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func TestMoveWizard_S3ToS3(t *testing.T) {
	// no t.Parallel — clientBuilder/prompter state
	fake := &mvWizardFake{buckets: []string{"src", "dst"}, objects: []string{"a.txt"}}
	restore := withFakeClient(fake)
	defer restore()

	// src bucket(0) -> object a.txt(0) -> dst bucket(1) -> dest key -> confirm.
	mock := tuitest.New().
		AddSelect(0).AddSelect(0).AddSelect(1).
		AddTextInput("renamed.txt").
		AddConfirm(true)

	f := cmdutil.NewTestFactory(mock)
	cmd := NewCmdMv(f, ioBufs())

	errOut := &bytes.Buffer{}
	io := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: errOut}
	if err := runMoveWizard(cmd, f, io, &cpOptions{}, nil); err != nil {
		t.Fatalf("runMoveWizard: %v", err)
	}
	if !strings.Contains(errOut.String(), "Move / rename an S3 object") {
		t.Errorf("missing wizard intro banner:\n%s", errOut.String())
	}
	if fake.deleted != "src/a.txt" {
		t.Errorf("source deleted = %q, want src/a.txt", fake.deleted)
	}
	if fake.copiedDst != "dst/renamed.txt" {
		t.Errorf("copy dest = %q, want dst/renamed.txt", fake.copiedDst)
	}
	if !strings.Contains(fake.copiedSrc, "src") || !strings.Contains(fake.copiedSrc, "a.txt") {
		t.Errorf("copy source = %q, want it to reference src/a.txt", fake.copiedSrc)
	}
}

// TestNavIdx covers the wizard index navigation that makes Esc=back work:
// success advances and applies, Esc steps back, Ctrl+C/real errors terminate.
func TestNavIdx(t *testing.T) {
	t.Parallel()
	boom := errors.New("io failure")
	cases := []struct {
		name      string
		err       error
		wantNext  int
		wantErr   error
		wantApply bool
	}{
		{"success advances + applies", nil, 3, nil, true},
		{"esc steps back", context.Canceled, 1, nil, false},
		{"ctrl+c terminates", tui.ErrInterrupted, -1, nil, false},
		{"real error propagates", boom, 2, boom, false},
	}
	for _, tc := range cases {
		applied := false
		next, out := navIdx(2, tc.err, func() { applied = true })
		if next != tc.wantNext || !errors.Is(out, tc.wantErr) || applied != tc.wantApply {
			t.Errorf("%s: navIdx = (next=%d err=%v applied=%v), want (%d %v %v)",
				tc.name, next, out, applied, tc.wantNext, tc.wantErr, tc.wantApply)
		}
	}
}

func TestSelectStep_EmptyValueExits(t *testing.T) {
	t.Parallel()
	applied := false
	next, err := selectStep(0, "", nil, func() { applied = true })
	if next != -1 || err != nil || applied {
		t.Errorf("selectStep(empty) = (next=%d err=%v applied=%v), want (-1, nil, false)", next, err, applied)
	}
	// Non-empty success still advances + applies.
	if next, _ := selectStep(0, "bucket", nil, func() { applied = true }); next != 1 || !applied {
		t.Errorf("selectStep(value) next=%d applied=%v, want 1/true", next, applied)
	}
}

func ioBufs() cmdutil.IOStreams {
	return cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
}
