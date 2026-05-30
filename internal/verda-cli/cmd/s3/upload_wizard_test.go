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
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// TestUploadWizard_SingleFileToRoot runs the wizard with the source given as an
// arg, picks the existing bucket, the bucket root, confirms, and verifies the
// upload lands at the file's basename.
func TestUploadWizard_SingleFileToRoot(t *testing.T) {
	// no t.Parallel — clientBuilder/transporter/prompter state
	tmp := t.TempDir()
	src := filepath.Join(tmp, "report.csv")
	if err := os.WriteFile(src, []byte("a,b,c\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	restore := withFakeClient(&fakeS3API{buckets: []s3types.Bucket{{Name: aws.String("b")}}})
	defer restore()
	fakeT := &cpFakeTransporter{}
	restoreT := withFakeTransporter(fakeT)
	defer restoreT()

	// bucket(0) -> location root(0) -> confirm
	mock := tuitest.New().AddSelect(0).AddSelect(0).AddConfirm(true)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(mock)
	opts := &cpOptions{Concurrency: defaultConcurrency}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	if err := runUploadWizard(cmd, f, cmdutil.IOStreams{Out: out, ErrOut: errOut}, opts, []string{src}); err != nil {
		t.Fatalf("runUploadWizard: %v", err)
	}

	if len(fakeT.uploads) != 1 {
		t.Fatalf("Upload calls = %d, want 1", len(fakeT.uploads))
	}
	if k := aws.ToString(fakeT.uploads[0].Key); k != "report.csv" {
		t.Errorf("upload key = %q, want report.csv", k)
	}
	if opts.Recursive {
		t.Errorf("Recursive should be false for a single file")
	}
}

// TestSelectUploadLocation_NewFolder verifies the '+ New folder…' path returns
// the typed prefix with a normalized trailing slash.
func TestSelectUploadLocation_NewFolder(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{} // no objects -> labels: [root, + New folder…]
	mock := tuitest.New().AddSelect(1).AddTextInput("models")
	f := cmdutil.NewTestFactory(mock)

	prefix, err := selectUploadLocation(context.Background(), f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}, fake, "b", "data")
	if err != nil {
		t.Fatalf("selectUploadLocation: %v", err)
	}
	if prefix != "models/" {
		t.Errorf("prefix = %q, want models/", prefix)
	}
}

// TestSelectUploadLocation_Root returns an empty prefix for the bucket-root choice.
func TestSelectUploadLocation_Root(t *testing.T) {
	// no t.Parallel
	fake := &fakeS3API{}
	mock := tuitest.New().AddSelect(0)
	f := cmdutil.NewTestFactory(mock)

	prefix, err := selectUploadLocation(context.Background(), f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}, fake, "b", "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if prefix != "" {
		t.Errorf("prefix = %q, want empty (root)", prefix)
	}
}

// TestClassifyNav covers the Esc=back / Ctrl+C=exit mapping the wizard relies on
// (tuitest can't synthesize cancel errors, so the nav logic is tested directly).
func TestClassifyNav(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	cases := []struct {
		name      string
		err       error
		firstStep bool
		back      bool
		exit      bool
		real      error
	}{
		{"nil advances", nil, false, false, false, nil},
		{"ctrl+c exits", tui.ErrInterrupted, false, false, true, nil},
		{"esc backs", context.Canceled, false, true, false, nil},
		{"esc on first step exits", context.Canceled, true, false, true, nil},
		{"real error propagates", boom, false, false, false, boom},
	}
	for _, tc := range cases {
		back, exit, real := classifyNav(tc.err, tc.firstStep)
		if back != tc.back || exit != tc.exit || real != tc.real {
			t.Errorf("%s: classifyNav = (back=%v exit=%v real=%v), want (%v %v %v)",
				tc.name, back, exit, real, tc.back, tc.exit, tc.real)
		}
	}
}
