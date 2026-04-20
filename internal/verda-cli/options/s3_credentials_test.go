package options

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadS3CredentialsHappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	content := `[default]
verda_client_id = api-id
verda_s3_access_key = AKIA123
verda_s3_secret_key = secret456
verda_s3_endpoint = https://objects.lab.verda.storage
verda_s3_region = us-east-1
verda_s3_auth_mode = credentials
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadS3CredentialsForProfile(path, "default")
	if err != nil {
		t.Fatalf("LoadS3CredentialsForProfile() error: %v", err)
	}

	if creds.AccessKey != "AKIA123" {
		t.Errorf("AccessKey = %q, want %q", creds.AccessKey, "AKIA123")
	}
	if creds.SecretKey != "secret456" {
		t.Errorf("SecretKey = %q, want %q", creds.SecretKey, "secret456")
	}
	if creds.Endpoint != "https://objects.lab.verda.storage" {
		t.Errorf("Endpoint = %q, want %q", creds.Endpoint, "https://objects.lab.verda.storage")
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", creds.Region, "us-east-1")
	}
	if creds.AuthMode != "credentials" {
		t.Errorf("AuthMode = %q, want %q", creds.AuthMode, "credentials")
	}
}

func TestLoadS3CredentialsMissingProfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	content := `[default]
verda_client_id = api-id
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadS3CredentialsForProfile(path, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestLoadS3CredentialsEmptyFile(t *testing.T) {
	t.Parallel()

	_, err := LoadS3CredentialsForProfile("", "default")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestLoadS3CredentialsPartial(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	// Only access key set — secret/endpoint/region empty.
	content := `[default]
verda_s3_access_key = AKIA123
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	creds, err := LoadS3CredentialsForProfile(path, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.AccessKey != "AKIA123" {
		t.Errorf("AccessKey = %q, want %q", creds.AccessKey, "AKIA123")
	}
	if creds.SecretKey != "" {
		t.Errorf("SecretKey = %q, want empty", creds.SecretKey)
	}
}
