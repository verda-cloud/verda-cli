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

package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

func TestWriteActiveProfile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(makeLocalTempDir(t), "config.yaml")
	if err := writeActiveProfile(path, "staging"); err != nil {
		t.Fatalf("writeActiveProfile() returned error: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test file
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	if string(data) == "" {
		t.Fatal("expected config file to be written")
	}
}

func TestResolveCredentialsFileUsesEnv(t *testing.T) {
	path := filepath.Join(makeLocalTempDir(t), "credentials")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", path)

	got, err := resolveCredentialsFile("")
	if err != nil {
		t.Fatalf("resolveCredentialsFile() returned error: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}
}

func TestResolveCredentialsFileUsesDefault(t *testing.T) {
	t.Parallel()

	got, err := resolveCredentialsFile("")
	if err != nil {
		t.Fatalf("resolveCredentialsFile() returned error: %v", err)
	}

	want, err := options.DefaultCredentialsFilePath()
	if err != nil {
		t.Fatalf("DefaultCredentialsFilePath() returned error: %v", err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func makeLocalTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp(".", "tmp-test-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}
