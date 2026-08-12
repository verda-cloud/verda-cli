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

// ResolveS3Credentials layers env over the file per field. These tests own the
// unit-level contract; cmd/objectstorage/helper_test.go owns the wiring.
func TestResolveS3CredentialsEnvOnly(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")
	t.Setenv("VERDA_S3_SECRET_KEY", "REPLACE_ME_ENV_SECRET")
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")
	t.Setenv("VERDA_S3_REGION", "eu-north-1")
	t.Setenv("VERDA_S3_AUTH_MODE", "credentials")

	creds, applied, err := ResolveS3Credentials(filepath.Join(t.TempDir(), "absent"), "default")
	if err != nil {
		t.Fatalf("ResolveS3Credentials() error: %v", err)
	}
	if !creds.HasCredentials() {
		t.Fatal("HasCredentials() = false for a complete env set")
	}
	if creds.Region != "eu-north-1" || creds.AuthMode != "credentials" {
		t.Errorf("Region = %q, AuthMode = %q", creds.Region, creds.AuthMode)
	}
	if len(applied) != 5 {
		t.Errorf("applied = %v, want all five variable names", applied)
	}
}

func TestResolveS3CredentialsPerFieldMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	content := `[default]
verda_s3_access_key = FILE_KEY
verda_s3_secret_key = FILE_SECRET
verda_s3_endpoint = https://file.example.invalid
verda_s3_region = us-east-1
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")

	creds, applied, err := ResolveS3Credentials(path, "default")
	if err != nil {
		t.Fatalf("ResolveS3Credentials() error: %v", err)
	}
	if creds.Endpoint != "https://env.example.invalid" {
		t.Errorf("Endpoint = %q, want the env value", creds.Endpoint)
	}
	if creds.AccessKey != "FILE_KEY" || creds.SecretKey != "FILE_SECRET" || creds.Region != "us-east-1" {
		t.Errorf("non-overridden fields lost: access_key_set=%t region=%q", creds.AccessKey != "", creds.Region)
	}
	if len(applied) != 1 || applied[0] != "VERDA_S3_ENDPOINT" {
		t.Errorf("applied = %v, want [VERDA_S3_ENDPOINT]", applied)
	}
}

func TestResolveS3CredentialsEmptyEnvIsUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	content := `[default]
verda_s3_access_key = FILE_KEY
verda_s3_secret_key = FILE_SECRET
verda_s3_endpoint = https://file.example.invalid
verda_s3_region = us-east-1
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VERDA_S3_REGION", "")
	t.Setenv("VERDA_S3_ENDPOINT", "   ")

	creds, applied, err := ResolveS3Credentials(path, "default")
	if err != nil {
		t.Fatalf("ResolveS3Credentials() error: %v", err)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q; empty env var blanked the file value", creds.Region)
	}
	if creds.Endpoint != "https://file.example.invalid" {
		t.Errorf("Endpoint = %q; whitespace-only env var blanked the file value", creds.Endpoint)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %v, want none", applied)
	}
}

// A missing file with an incomplete env must still surface the load error, so
// callers keep their "not configured" path.
func TestResolveS3CredentialsMissingFileIncompleteEnv(t *testing.T) {
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")

	creds, applied, err := ResolveS3Credentials(filepath.Join(t.TempDir(), "absent"), "default")
	if err == nil {
		t.Fatal("expected the file-load error to survive an incomplete env")
	}
	if creds == nil {
		t.Fatal("creds must never be nil, even with an error")
	}
	if creds.HasCredentials() {
		t.Error("HasCredentials() = true with only an access key")
	}
	if len(applied) != 1 {
		t.Errorf("applied = %v, want [VERDA_S3_ACCESS_KEY]", applied)
	}
}

func TestResolveS3CredentialsEmptyPathEnvOnly(t *testing.T) {
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")
	t.Setenv("VERDA_S3_SECRET_KEY", "REPLACE_ME_ENV_SECRET")
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")

	creds, _, err := ResolveS3Credentials("", "default")
	if err != nil {
		t.Fatalf("ResolveS3Credentials(\"\") error: %v", err)
	}
	if !creds.HasCredentials() {
		t.Error("HasCredentials() = false; env-only resolution with no file path failed")
	}
}
