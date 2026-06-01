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
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

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

// TestBuildMoveFlow_CollectsSelections drives the engine flow (no source arg) and
// asserts the steps populate the wizard state: source bucket → object → dest
// bucket (existing) → dest key. The dest-bucket choices are [src, dst, +create],
// so index 1 selects "dst" and the new-bucket step is skipped.
func TestBuildMoveFlow_CollectsSelections(t *testing.T) {
	// no t.Parallel — clientBuilder/prompter state via fake
	fake := &mvWizardFake{buckets: []string{"src", "dst"}, objects: []string{"a.txt"}}
	f := cmdutil.NewTestFactory(tuitest.New())
	st := &moveWizardState{}

	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.SelectResult(0),           // source bucket: src
			wizard.SelectResult(0),           // source object: a.txt
			wizard.SelectResult(1),           // dest bucket: dst
			wizard.TextResult("renamed.txt"), // dest key
		),
	)
	if err := engine.Run(context.Background(), buildMoveFlow(f, fake, st)); err != nil {
		t.Fatalf("engine Run: %v", err)
	}
	if st.srcBucket != "src" || st.srcKey != "a.txt" || st.dstBucket != "dst" || st.dstKey != "renamed.txt" {
		t.Errorf("state = %+v, want src/a.txt -> dst/renamed.txt", st)
	}
}

// TestFinalizeMove_S3ToS3 covers the post-wizard execution: confirm → CopyObject
// on the destination + DeleteObject on the source.
func TestFinalizeMove_S3ToS3(t *testing.T) {
	// no t.Parallel — clientBuilder/prompter state
	fake := &mvWizardFake{}
	restore := withFakeClient(fake)
	defer restore()

	f := cmdutil.NewTestFactory(tuitest.New().AddConfirm(true))
	cmd := NewCmdMv(f, ioBufs())
	st := &moveWizardState{srcBucket: "src", srcKey: "a.txt", dstBucket: "dst", dstKey: "renamed.txt"}

	if err := finalizeMove(context.Background(), cmd, f, ioBufs(), fake, &cpOptions{}, st); err != nil {
		t.Fatalf("finalizeMove: %v", err)
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

func ioBufs() cmdutil.IOStreams {
	return cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
}
