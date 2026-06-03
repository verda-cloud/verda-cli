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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// TestUploadsCmds_NoArgShowsHelp verifies the cleanup commands render full help
// (not a terse "accepts 1 arg(s)") and exit cleanly when the bucket is omitted.
func TestUploadsCmds_NoArgShowsHelp(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(cmdutil.Factory, cmdutil.IOStreams) *cobra.Command
	}{
		{"ls-uploads", NewCmdLsUploads},
		{"abort-uploads", NewCmdAbortUploads},
	} {
		out := &bytes.Buffer{}
		f := cmdutil.NewTestFactory(nil)
		cmd := tc.make(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
		cmd.SetArgs([]string{})
		cmd.SetContext(context.Background())
		cmd.SetOut(out)
		if err := cmd.Execute(); err != nil {
			t.Errorf("%s: no-arg should show help, not error: %v", tc.name, err)
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Errorf("%s: expected help with Usage:, got:\n%s", tc.name, out.String())
		}
	}
}

// uploadsFakeAPI serves ListMultipartUploads / ListParts and records
// AbortMultipartUpload calls for the cleanup-command tests.
type uploadsFakeAPI struct {
	API

	listUploadsPages []*s3.ListMultipartUploadsOutput
	listUploadsCalls int
	listUploadsIns   []*s3.ListMultipartUploadsInput

	// partsByUpload maps an UploadId to its parts (for ListParts size sums).
	partsByUpload map[string][]s3types.Part

	abortIns []*s3.AbortMultipartUploadInput

	listUploadsErr error
}

func (u *uploadsFakeAPI) ListMultipartUploads(ctx context.Context, in *s3.ListMultipartUploadsInput, opts ...func(*s3.Options)) (*s3.ListMultipartUploadsOutput, error) {
	if u.listUploadsErr != nil {
		return nil, u.listUploadsErr
	}
	u.listUploadsIns = append(u.listUploadsIns, in)
	if u.listUploadsCalls >= len(u.listUploadsPages) {
		return &s3.ListMultipartUploadsOutput{IsTruncated: aws.Bool(false)}, nil
	}
	page := u.listUploadsPages[u.listUploadsCalls]
	u.listUploadsCalls++
	return page, nil
}

func (u *uploadsFakeAPI) ListParts(ctx context.Context, in *s3.ListPartsInput, opts ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	id := aws.ToString(in.UploadId)
	parts := u.partsByUpload[id]
	return &s3.ListPartsOutput{Parts: parts, IsTruncated: aws.Bool(false)}, nil
}

func (u *uploadsFakeAPI) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput, opts ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	u.abortIns = append(u.abortIns, in)
	return &s3.AbortMultipartUploadOutput{}, nil
}

func uploadPage(uploads ...s3types.MultipartUpload) *s3.ListMultipartUploadsOutput {
	return &s3.ListMultipartUploadsOutput{Uploads: uploads, IsTruncated: aws.Bool(false)}
}

func mpu(key, id string, initiated time.Time) s3types.MultipartUpload {
	return s3types.MultipartUpload{Key: aws.String(key), UploadId: aws.String(id), Initiated: aws.Time(initiated)}
}

func TestLsUploads_ListsWithSize(t *testing.T) {
	// no t.Parallel — clientBuilder mutation
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(
				mpu("a.bin", "u-a", now.Add(-time.Hour)),
				mpu("b.bin", "u-b", now.Add(-2*time.Hour)),
			),
		},
		partsByUpload: map[string][]s3types.Part{
			"u-a": {{PartNumber: aws.Int32(1), Size: aws.Int64(100)}, {PartNumber: aws.Int32(2), Size: aws.Int64(50)}},
			"u-b": {{PartNumber: aws.Int32(1), Size: aws.Int64(7)}},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLsUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stdout := out.String()
	for _, want := range []string{"a.bin", "u-a", "b.bin", "2 in-progress upload(s)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestLsUploads_JSON(t *testing.T) {
	// no t.Parallel
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(mpu("a.bin", "u-a", now)),
		},
		partsByUpload: map[string][]s3types.Part{
			"u-a": {{PartNumber: aws.Int32(1), Size: aws.Int64(123)}},
		},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	cmd := NewCmdLsUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	stdout := out.String()
	for _, want := range []string{`"upload_id": "u-a"`, `"size": 123`, `"key": "a.bin"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("json missing %q:\n%s", want, stdout)
		}
	}
}

func TestLsUploads_Empty(t *testing.T) {
	// no t.Parallel
	fake := &uploadsFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLsUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "No in-progress multipart uploads") {
		t.Errorf("expected empty message:\n%s", out.String())
	}
}

// TestLsUploads_TruncatedEmptyMarkerNoLoop guards against the IsTruncated-with-
// empty-marker pagination loop: the command must stop and mark truncated.
func TestLsUploads_TruncatedEmptyMarkerNoLoop(t *testing.T) {
	// no t.Parallel
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			{
				Uploads:     []s3types.MultipartUpload{mpu("a.bin", "u-a", time.Now())},
				IsTruncated: aws.Bool(true), // truncated...
				// ...but no NextKeyMarker / NextUploadIdMarker -> must not loop.
			},
		},
		partsByUpload: map[string][]s3types.Part{"u-a": {{Size: aws.Int64(1)}}},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdLsUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if fake.listUploadsCalls != 1 {
		t.Errorf("ListMultipartUploads calls = %d, want 1 (must not loop on empty marker)", fake.listUploadsCalls)
	}
}

func TestAbortUploads_OlderThanFilter(t *testing.T) {
	// no t.Parallel
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(
				mpu("old.bin", "u-old", now.Add(-10*24*time.Hour)), // 10 days old -> abort
				mpu("new.bin", "u-new", now.Add(-1*time.Hour)),     // 1 hour old -> keep
			),
		},
		partsByUpload: map[string][]s3types.Part{},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdAbortUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket", "--older-than", "7d", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.abortIns) != 1 {
		t.Fatalf("Abort calls = %d, want 1 (only the 10-day-old upload)", len(fake.abortIns))
	}
	if got := aws.ToString(fake.abortIns[0].UploadId); got != "u-old" {
		t.Errorf("aborted UploadId = %q, want u-old", got)
	}
}

func TestAbortUploads_KeyFilter(t *testing.T) {
	// no t.Parallel
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(
				mpu("a.bin", "u-a", now),
				mpu("b.bin", "u-b", now),
			),
		},
		partsByUpload: map[string][]s3types.Part{},
	}
	restore := withFakeClient(fake)
	defer restore()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdAbortUploads(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket", "--key", "b.bin", "--yes"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.abortIns) != 1 || aws.ToString(fake.abortIns[0].UploadId) != "u-b" {
		ids := make([]string, 0, len(fake.abortIns))
		for _, in := range fake.abortIns {
			ids = append(ids, aws.ToString(in.UploadId))
		}
		sort.Strings(ids)
		t.Fatalf("aborted = %v, want [u-b]", ids)
	}
}

func TestAbortUploads_AgentModeNeedsYes(t *testing.T) {
	// no t.Parallel
	fake := &uploadsFakeAPI{}
	restore := withFakeClient(fake)
	defer restore()

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	cmd := NewCmdAbortUploads(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"s3://my-bucket"})
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
	if len(fake.abortIns) != 0 {
		t.Errorf("Abort should not be called when confirmation required, got %d", len(fake.abortIns))
	}
}

// TestAbortUploads_InteractiveConfirm_Yes drives the TTY confirmation path.
func TestAbortUploads_InteractiveConfirm_Yes(t *testing.T) {
	// no t.Parallel
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(mpu("a.bin", "u-a", now)),
		},
		partsByUpload: map[string][]s3types.Part{},
	}
	restore := withFakeClient(fake)
	defer restore()

	mock := tuitest.New().AddConfirm(true)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)
	cmd := NewCmdAbortUploads(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fake.abortIns) != 1 {
		t.Fatalf("Abort calls = %d, want 1 (confirmed)", len(fake.abortIns))
	}
	if !strings.Contains(errOut.String(), "abort 1 in-progress upload(s)") {
		t.Errorf("expected destructive warning on stderr:\n%s", errOut.String())
	}
}

// TestAbortUploads_InteractiveConfirm_No verifies declining aborts nothing.
func TestAbortUploads_InteractiveConfirm_No(t *testing.T) {
	// no t.Parallel
	now := time.Now()
	fake := &uploadsFakeAPI{
		listUploadsPages: []*s3.ListMultipartUploadsOutput{
			uploadPage(mpu("a.bin", "u-a", now)),
		},
		partsByUpload: map[string][]s3types.Part{},
	}
	restore := withFakeClient(fake)
	defer restore()

	mock := tuitest.New().AddConfirm(false)
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)
	cmd := NewCmdAbortUploads(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd.SetArgs([]string{"s3://my-bucket"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute should not error on declined confirmation: %v", err)
	}
	if len(fake.abortIns) != 0 {
		t.Errorf("Abort should not be called when declined, got %d", len(fake.abortIns))
	}
	if !strings.Contains(errOut.String(), "Canceled") {
		t.Errorf("expected 'Canceled.' on stderr:\n%s", errOut.String())
	}
}

func TestParseByteSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"32MiB", 32 << 20, false},
		{"8M", 8 << 20, false},
		{"5mib", 5 << 20, false},
		{"1073741824", 1073741824, false},
		{"1GiB", 1 << 30, false},
		{"bad", 0, true},
		{"-5MiB", 0, true},
	}
	for _, tc := range cases {
		got, err := parseByteSize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseByteSize(%q) = %d, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseByteSize(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseOlderThan(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"12h", 12 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"1.5d", 36 * time.Hour, false},
		{"bad", 0, true},
	}
	for _, tc := range cases {
		got, err := parseOlderThan(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseOlderThan(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseOlderThan(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseOlderThan(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
