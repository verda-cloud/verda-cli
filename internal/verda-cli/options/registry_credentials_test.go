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

package options

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRegistryCredentialsForProfile_HappyPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	content := `[default]
verda_registry_username = vcr-abc+name
verda_registry_secret = s3cret
verda_registry_endpoint = registry.example.com
verda_registry_project_id = abc
verda_registry_expires_at = 2099-01-01T00:00:00Z
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadRegistryCredentialsForProfile(path, "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Username != "vcr-abc+name" || got.Secret != "s3cret" || got.Endpoint != "registry.example.com" {
		t.Errorf("unexpected creds: %+v", got)
	}
	if got.ProjectID != "abc" {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, "abc")
	}
	if got.ExpiresAt.Year() != 2099 {
		t.Errorf("expires_at parsing: got %v", got.ExpiresAt)
	}
	if !got.HasCredentials() {
		t.Error("HasCredentials should be true")
	}
}

func TestRegistryCredentials_Expired(t *testing.T) {
	t.Parallel()

	past := &RegistryCredentials{ExpiresAt: time.Now().Add(-1 * time.Hour)}
	if !past.IsExpired() {
		t.Error("past expiry should report expired")
	}
	future := &RegistryCredentials{ExpiresAt: time.Now().Add(1 * time.Hour)}
	if future.IsExpired() {
		t.Error("future expiry should not report expired")
	}
	zero := &RegistryCredentials{} // zero time: treat as non-expiring (legacy rows)
	if zero.IsExpired() {
		t.Error("zero expires_at should not be treated as expired")
	}
}

func TestLoadRegistryCredentialsForProfile_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadRegistryCredentialsForProfile("", "default")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty path: got %v, want os.ErrNotExist", err)
	}

	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	if _, err := LoadRegistryCredentialsForProfile(missing, "default"); err == nil {
		t.Fatal("expected error for non-existent file path")
	}
}

func TestLoadRegistryCredentialsForProfile_MissingProfile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	content := `[default]
verda_registry_username = user
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadRegistryCredentialsForProfile(path, "nonexistent"); err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestLoadRegistryCredentialsForProfile_EmptyFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	content := `[default]
verda_client_id = api-id
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadRegistryCredentialsForProfile(path, "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Username != "" || got.Secret != "" || got.Endpoint != "" || got.ProjectID != "" {
		t.Errorf("expected empty fields, got %+v", got)
	}
	if !got.ExpiresAt.IsZero() {
		t.Errorf("expected zero ExpiresAt, got %v", got.ExpiresAt)
	}
	if got.HasCredentials() {
		t.Error("HasCredentials should be false for empty fields")
	}
}

func TestLoadRegistryCredentialsForProfile_MalformedExpiry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	content := `[default]
verda_registry_username = user
verda_registry_secret = secret
verda_registry_endpoint = registry.example.com
verda_registry_expires_at = not-a-timestamp
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadRegistryCredentialsForProfile(path, "default")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !got.ExpiresAt.IsZero() {
		t.Errorf("malformed expiry should leave ExpiresAt zero, got %v", got.ExpiresAt)
	}
	if !got.HasCredentials() {
		t.Error("HasCredentials should still be true despite malformed expiry")
	}
}

func TestRegistryCredentials_DaysRemaining(t *testing.T) {
	t.Parallel()

	past := &RegistryCredentials{ExpiresAt: time.Now().Add(-48 * time.Hour)}
	if d := past.DaysRemaining(); d >= 0 {
		t.Errorf("past DaysRemaining should be negative, got %d", d)
	}

	future := &RegistryCredentials{ExpiresAt: time.Now().Add(36 * time.Hour)}
	if d := future.DaysRemaining(); d != 1 {
		t.Errorf("future DaysRemaining ~1, got %d", d)
	}

	zero := &RegistryCredentials{}
	// Zero ExpiresAt uses a large sentinel (>> any realistic day count)
	// so callers can treat credentials as non-expiring.
	if d := zero.DaysRemaining(); d < 1_000_000 {
		t.Errorf("zero DaysRemaining should be a large sentinel, got %d", d)
	}
}
