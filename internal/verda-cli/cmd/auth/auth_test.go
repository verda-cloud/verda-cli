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
