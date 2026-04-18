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

//go:build integration

// S3 data-plane smoke test.
//
// This test exercises a full S3 round-trip (mb / cp / ls / cp / rm / rb)
// against a live S3-compatible endpoint (MinIO locally or Verda staging).
//
// It is gated by both the `integration` build tag AND the
// VERDA_S3_INTEGRATION=1 env var — so it never runs under `make test`,
// and a developer running the normal integration suite without S3
// credentials handy sees SKIP rather than a failure.
//
// Required env (all must be set when VERDA_S3_INTEGRATION=1):
//
//	VERDA_S3_ENDPOINT      e.g. http://127.0.0.1:9000 or https://s3.staging.verda.cloud
//	VERDA_S3_ACCESS_KEY
//	VERDA_S3_SECRET_KEY
//	VERDA_S3_REGION        e.g. us-east-1
//
// Run:
//
//	make test-s3-integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// s3Env holds the resolved live-endpoint credentials. Tests call requireS3Env
// to populate it; if the feature-flag env var is unset, requireS3Env skips
// the test outright.
type s3Env struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	// CredentialsFile is the path to the temp INI file the test writes, which
	// the verda binary reads via VERDA_SHARED_CREDENTIALS_FILE.
	CredentialsFile string
	// Profile is the profile name used inside the temp credentials file.
	Profile string
}

// requireS3Env returns the live-S3 environment, or calls t.Skip if the
// feature flag is off or required env vars are missing.
func requireS3Env(t *testing.T) s3Env {
	t.Helper()

	if os.Getenv("VERDA_S3_INTEGRATION") != "1" {
		t.Skip("VERDA_S3_INTEGRATION not set; skipping S3 live-endpoint smoke test")
	}

	env := s3Env{
		Endpoint:  os.Getenv("VERDA_S3_ENDPOINT"),
		AccessKey: os.Getenv("VERDA_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("VERDA_S3_SECRET_KEY"),
		Region:    os.Getenv("VERDA_S3_REGION"),
		Profile:   "integration-s3",
	}

	var missing []string
	if env.Endpoint == "" {
		missing = append(missing, "VERDA_S3_ENDPOINT")
	}
	if env.AccessKey == "" {
		missing = append(missing, "VERDA_S3_ACCESS_KEY")
	}
	if env.SecretKey == "" {
		missing = append(missing, "VERDA_S3_SECRET_KEY")
	}
	if env.Region == "" {
		missing = append(missing, "VERDA_S3_REGION")
	}
	if len(missing) > 0 {
		t.Skipf("VERDA_S3_INTEGRATION=1 but missing required env: %s", strings.Join(missing, ", "))
	}

	// Write a minimal credentials file the verda binary can read.
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	contents := fmt.Sprintf(`[%s]
verda_s3_access_key = %s
verda_s3_secret_key = %s
verda_s3_endpoint   = %s
verda_s3_region     = %s
verda_s3_auth_mode  = credentials
`, env.Profile, env.AccessKey, env.SecretKey, env.Endpoint, env.Region)

	if err := os.WriteFile(credsPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp credentials file: %v", err)
	}
	env.CredentialsFile = credsPath

	return env
}

// runS3 runs the verda binary with args, injecting the temp credentials file
// via VERDA_SHARED_CREDENTIALS_FILE and forcing --auth.profile to the
// integration profile so the s3 credential lookup resolves.
func runS3(t *testing.T, env s3Env, args ...string) cliResult {
	t.Helper()
	start := time.Now()

	fullArgs := append([]string{"--auth.profile", env.Profile}, args...)

	cmd := exec.Command(verdaBin(), fullArgs...)
	cmd.Env = append(os.Environ(),
		"VERDA_SHARED_CREDENTIALS_FILE="+env.CredentialsFile,
		// Unset any ambient profile override so --auth.profile wins cleanly.
		"VERDA_PROFILE=",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run verda %s: %v", strings.Join(fullArgs, " "), err)
		}
	}

	return cliResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration,
	}
}

// TestS3RoundTrip exercises mb / cp upload / ls / cp download / rm / rb
// against a live S3-compatible endpoint.
func TestS3RoundTrip(t *testing.T) {
	env := requireS3Env(t)

	bucket := fmt.Sprintf("verda-cli-test-%d", time.Now().UnixNano())
	bucketURI := "s3://" + bucket
	objectKey := "test.txt"
	objectURI := bucketURI + "/" + objectKey

	workDir := t.TempDir()
	srcPath := filepath.Join(workDir, "tmpfile")
	dstPath := filepath.Join(workDir, "downloaded.txt")

	payload := []byte("verda s3 integration smoke test\n")
	if err := os.WriteFile(srcPath, payload, 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Best-effort cleanup in case any step between mb and rb fails.
	// We emit rb --force --yes which empties the bucket if needed.
	t.Cleanup(func() {
		r := runS3(t, env, "s3", "rb", bucketURI, "--force", "--yes")
		if r.ExitCode != 0 {
			t.Logf("cleanup: rb %s exited %d: %s", bucketURI, r.ExitCode, r.Stderr)
		}
	})

	// 1. Make bucket.
	if r := runS3(t, env, "s3", "mb", bucketURI); r.ExitCode != 0 {
		t.Fatalf("s3 mb failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// 2. Upload.
	if r := runS3(t, env, "s3", "cp", srcPath, objectURI); r.ExitCode != 0 {
		t.Fatalf("s3 cp upload failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// 3. List, assert the key appears.
	r := runS3(t, env, "s3", "ls", bucketURI+"/", "--recursive")
	if r.ExitCode != 0 {
		t.Fatalf("s3 ls failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
	if !strings.Contains(r.Stdout, objectKey) {
		t.Fatalf("s3 ls output does not contain key %q\nstdout: %s", objectKey, r.Stdout)
	}

	// 4. Download.
	if r := runS3(t, env, "s3", "cp", objectURI, dstPath); r.ExitCode != 0 {
		t.Fatalf("s3 cp download failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	downloaded, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Fatalf("downloaded content mismatch\nwant: %q\ngot:  %q", payload, downloaded)
	}

	// 5. Delete object.
	if r := runS3(t, env, "s3", "rm", objectURI, "--yes"); r.ExitCode != 0 {
		t.Fatalf("s3 rm failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	// 6. Remove bucket.
	if r := runS3(t, env, "s3", "rb", bucketURI, "--yes"); r.ExitCode != 0 {
		t.Fatalf("s3 rb failed (exit %d)\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}
}
