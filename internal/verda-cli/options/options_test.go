package options

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSharedCredentials(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
client_id = default-id
client_secret = default-secret

[dev]
client_id = dev-id
client_secret = dev-secret
token = dev-token
`)

	creds, err := loadSharedCredentials(path, "dev")
	if err != nil {
		t.Fatalf("loadSharedCredentials() returned error: %v", err)
	}

	if creds.ClientID != "dev-id" || creds.ClientSecret != "dev-secret" || creds.BearerToken != "dev-token" {
		t.Fatalf("unexpected credentials: %+v", creds)
	}
}

func TestOptionsCompleteLoadsSharedCredentials(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
client_id = default-id
client_secret = default-secret
token = default-token
`)

	opts := &Options{
		Server:  "https://api.verda.com/v1",
		Timeout: 30,
		AuthOptions: &AuthOptions{
			CredentialsFile: path,
			Profile:         "default",
		},
	}

	opts.Complete()

	if opts.AuthOptions.ClientID != "default-id" {
		t.Fatalf("expected client ID from shared credentials, got %q", opts.AuthOptions.ClientID)
	}
	if opts.AuthOptions.ClientSecret != "default-secret" {
		t.Fatalf("expected client secret from shared credentials, got %q", opts.AuthOptions.ClientSecret)
	}
	if opts.AuthOptions.BearerToken != "default-token" {
		t.Fatalf("expected bearer token from shared credentials, got %q", opts.AuthOptions.BearerToken)
	}
}

func TestOptionsCompleteKeepsExplicitValues(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
client_id = shared-id
client_secret = shared-secret
`)

	opts := &Options{
		Server:  "https://api.verda.com/v1",
		Timeout: 30,
		AuthOptions: &AuthOptions{
			ClientID:        "flag-id",
			CredentialsFile: path,
			Profile:         "default",
		},
	}

	opts.Complete()

	if opts.AuthOptions.ClientID != "flag-id" {
		t.Fatalf("expected explicit client ID to win, got %q", opts.AuthOptions.ClientID)
	}
	if opts.AuthOptions.ClientSecret != "shared-secret" {
		t.Fatalf("expected missing client secret to come from shared credentials, got %q", opts.AuthOptions.ClientSecret)
	}
}

func TestOptionsValidateReturnsProfileError(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
client_id = default-id
client_secret = default-secret
`)

	opts := &Options{
		Server:  "https://api.verda.com/v1",
		Timeout: 30,
		AuthOptions: &AuthOptions{
			CredentialsFile: path,
			Profile:         "missing",
		},
	}

	opts.Complete()

	if err := opts.Validate(); err == nil {
		t.Fatal("expected Validate() to return a missing-profile error")
	}
}

func writeCredentialsFile(t *testing.T, content string) string {
	t.Helper()

	dir := makeLocalTempDir(t)
	path := filepath.Join(dir, "credentials")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}
	return path
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
