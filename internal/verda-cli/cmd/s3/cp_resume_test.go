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
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// writeCpSrc writes size bytes to a file under t.TempDir and returns its path.
func writeCpSrc(t *testing.T, size int64) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write src: %v", err)
	}
	return path
}

// TestCp_Upload_LargeFile_RoutesResumable verifies a single file larger than
// the (auto) part size is uploaded via the custom multipart loop (CreateMpu +
// UploadPart + Complete) rather than the transfer-manager PutObject path.
func TestCp_Upload_LargeFile_RoutesResumable(t *testing.T) {
	// no t.Parallel — clientBuilder/transporterBuilder mutation
	withTempVerdaHome(t)
	src := writeCpSrc(t, 3*minPartSize+100) // 4 parts at the 5MiB floor

	fake := newFakeMPUploadAPI()
	restore := withFakeClient(fake)
	defer restore()
	// The transporter must NOT be used for a large single file.
	tr := &cpFakeTransporter{}
	restoreT := withFakeTransporter(tr)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/big.bin", "--part-size", "5MiB", "--concurrency", "1"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(tr.uploads) != 0 {
		t.Errorf("transfer-manager Upload must not be used for a large file, got %d", len(tr.uploads))
	}
	if fake.createCalls != 1 {
		t.Errorf("CreateMultipartUpload calls = %d, want 1", fake.createCalls)
	}
	if fake.uploadCalls != 4 {
		t.Errorf("UploadPart calls = %d, want 4", fake.uploadCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	if !strings.Contains(out.String(), "uploaded") {
		t.Errorf("stdout missing 'uploaded':\n%s", out.String())
	}
	// Key must be the resolved single-target key.
	if len(fake.completedSet) != 4 {
		t.Errorf("completed parts = %d, want 4", len(fake.completedSet))
	}
}

// TestCp_Upload_SmallFile_StaysOnTransferManager verifies that a file at or
// below the part size still goes through the transfer-manager PutObject path
// (no multipart machinery, no checkpoint).
func TestCp_Upload_SmallFile_StaysOnTransferManager(t *testing.T) {
	// no t.Parallel
	withTempVerdaHome(t)
	src := writeCpSrc(t, 1024) // well under 5MiB

	fake := newFakeMPUploadAPI()
	restore := withFakeClient(fake)
	defer restore()
	tr := &cpFakeTransporter{}
	restoreT := withFakeTransporter(tr)
	defer restoreT()

	out := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/small.bin"})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(tr.uploads) != 1 {
		t.Errorf("transfer-manager Upload calls = %d, want 1 (small file)", len(tr.uploads))
	}
	if fake.createCalls != 0 {
		t.Errorf("CreateMultipartUpload must not be called for a small file, got %d", fake.createCalls)
	}
}

// TestCp_Upload_LargeFile_Resume drives the cp command twice against the same
// fake server: the first run breaks after 2 parts (checkpoint persists), the
// second resumes, uploads only the missing parts, prints the resume line, and
// completes.
func TestCp_Upload_LargeFile_Resume(t *testing.T) {
	// no t.Parallel
	withTempVerdaHome(t)
	src := writeCpSrc(t, 3*minPartSize+100) // 4 parts

	fake := newFakeMPUploadAPI()
	fake.failAfterPart = 2
	restore := withFakeClient(fake)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	// First run: expect failure, checkpoint persisted with 2 parts.
	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/big.bin", "--part-size", "5MiB", "--concurrency", "1"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected first-run failure")
	}
	if fake.completeCalls != 0 {
		t.Fatalf("Complete must not be called on first-run failure, got %d", fake.completeCalls)
	}

	// Second run: same fake (parts 1-2 retained), so only 3,4 should upload.
	fake.failAfterPart = 0
	fake.uploadCalls = 0
	fake.uploadOrder = nil

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd2 := NewCmdCp(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	cmd2.SetArgs([]string{src, "s3://my-bucket/dest/big.bin", "--part-size", "5MiB", "--concurrency", "1"})
	cmd2.SetContext(context.Background())
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("resume Execute: %v", err)
	}
	if fake.createCalls != 1 {
		t.Errorf("Create calls = %d, want 1 (resume must not re-create)", fake.createCalls)
	}
	if fake.uploadCalls != 2 {
		t.Errorf("resume UploadPart calls = %d, want 2 (only missing 3,4)", fake.uploadCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	if !strings.Contains(errOut.String(), "Resuming upload (2/4 parts already on server)") {
		t.Errorf("expected resume line on stderr:\n%s", errOut.String())
	}
}

// TestCp_Upload_LargeFile_NoResume verifies --no-resume aborts the stale upload
// and restarts fresh, skipping the resume reconcile.
func TestCp_Upload_LargeFile_NoResume(t *testing.T) {
	// no t.Parallel
	withTempVerdaHome(t)
	src := writeCpSrc(t, minPartSize+10) // 2 parts

	fake := newFakeMPUploadAPI()
	fake.failAfterPart = 1
	restore := withFakeClient(fake)
	defer restore()
	restoreT := withFakeTransporter(&cpFakeTransporter{})
	defer restoreT()

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdCp(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{src, "s3://my-bucket/dest/big.bin", "--part-size", "5MiB", "--concurrency", "1"})
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected first-run failure")
	}

	fake.failAfterPart = 0
	cmd2 := NewCmdCp(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd2.SetArgs([]string{src, "s3://my-bucket/dest/big.bin", "--part-size", "5MiB", "--concurrency", "1", "--no-resume"})
	cmd2.SetContext(context.Background())
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("no-resume Execute: %v", err)
	}
	if fake.abortCalls != 1 {
		t.Errorf("Abort calls = %d, want 1 (--no-resume aborts stale)", fake.abortCalls)
	}
	if fake.createCalls != 2 {
		t.Errorf("Create calls = %d, want 2 (one per run)", fake.createCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
}
