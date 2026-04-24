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

	"gopkg.in/ini.v1"
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

func TestWriteRegistryCredentialsToProfile_AllFiveKeys(t *testing.T) {
	// Isolate HOME so the write helper's EnsureVerdaDir can't touch the
	// developer's real ~/.verda directory during tests.
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)

	path := filepath.Join(dir, "credentials")
	expires := time.Date(2099, 1, 2, 3, 4, 5, 0, time.UTC)

	creds := &RegistryCredentials{
		Username:  "vcr-abc+cli",
		Secret:    "s3cret",
		Endpoint:  "vccr.io",
		ProjectID: "abc",
		ExpiresAt: expires,
	}

	if err := WriteRegistryCredentialsToProfile(path, "default", creds); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load back: %v", err)
	}
	sec := cfg.Section("default")

	want := map[string]string{
		"verda_registry_username":   "vcr-abc+cli",
		"verda_registry_secret":     "s3cret",
		"verda_registry_endpoint":   "vccr.io",
		"verda_registry_project_id": "abc",
		"verda_registry_expires_at": expires.Format(time.RFC3339),
	}
	for k, v := range want {
		if got := sec.Key(k).String(); got != v {
			t.Errorf("key %q: got %q, want %q", k, got, v)
		}
	}
}

func TestWriteRegistryCredentialsToProfile_PreservesExistingKeysInSameSection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)

	path := filepath.Join(dir, "credentials")
	existing := `[default]
verda_client_id = my-api-id
verda_client_secret = my-api-secret
verda_s3_access_key = AKIAEXAMPLE
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	creds := &RegistryCredentials{
		Username:  "vcr-abc+cli",
		Secret:    "s3cret",
		Endpoint:  "vccr.io",
		ProjectID: "abc",
		ExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := WriteRegistryCredentialsToProfile(path, "default", creds); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sec := cfg.Section("default")
	if sec.Key("verda_client_id").String() != "my-api-id" {
		t.Error("API client_id was clobbered")
	}
	if sec.Key("verda_client_secret").String() != "my-api-secret" {
		t.Error("API client_secret was clobbered")
	}
	if sec.Key("verda_s3_access_key").String() != "AKIAEXAMPLE" {
		t.Error("S3 access_key was clobbered")
	}
	if sec.Key("verda_registry_username").String() != "vcr-abc+cli" {
		t.Error("registry_username not written alongside existing keys")
	}
}

func TestWriteRegistryCredentialsToProfile_PreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)

	path := filepath.Join(dir, "credentials")
	existing := `[default]
verda_client_id = default-id

[staging]
verda_client_id = staging-id
verda_s3_endpoint = https://staging.s3.example.com
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	creds := &RegistryCredentials{
		Username:  "vcr-prod+cli",
		Secret:    "prodsecret",
		Endpoint:  "vccr.io",
		ProjectID: "prod",
		ExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := WriteRegistryCredentialsToProfile(path, "default", creds); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// default got the registry keys.
	if cfg.Section("default").Key("verda_registry_username").String() != "vcr-prod+cli" {
		t.Error("registry keys not written to [default]")
	}
	// staging is verbatim.
	staging := cfg.Section("staging")
	if staging.Key("verda_client_id").String() != "staging-id" {
		t.Errorf("[staging] verda_client_id changed: %q", staging.Key("verda_client_id").String())
	}
	if staging.Key("verda_s3_endpoint").String() != "https://staging.s3.example.com" {
		t.Errorf("[staging] verda_s3_endpoint changed: %q", staging.Key("verda_s3_endpoint").String())
	}
	if staging.HasKey("verda_registry_username") {
		t.Error("registry keys leaked into [staging]")
	}
}

func TestWriteRegistryCredentialsToProfile_RFC3339Timestamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)

	path := filepath.Join(dir, "credentials")
	expires := time.Date(2030, 6, 15, 12, 30, 45, 0, time.UTC)

	if err := WriteRegistryCredentialsToProfile(path, "default", &RegistryCredentials{
		Username:  "u",
		Secret:    "s",
		Endpoint:  "vccr.io",
		ProjectID: "p",
		ExpiresAt: expires,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := cfg.Section("default").Key("verda_registry_expires_at").String()
	if raw != expires.Format(time.RFC3339) {
		t.Errorf("expires_at not RFC3339: got %q", raw)
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse back: %v", err)
	}
	if !parsed.Equal(expires) {
		t.Errorf("round-trip mismatch: got %v, want %v", parsed, expires)
	}
}

func TestWriteRegistryCredentialsToProfile_ZeroExpiresAtOmitted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)

	path := filepath.Join(dir, "credentials")

	if err := WriteRegistryCredentialsToProfile(path, "default", &RegistryCredentials{
		Username:  "u",
		Secret:    "s",
		Endpoint:  "vccr.io",
		ProjectID: "p",
		// ExpiresAt is zero
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sec := cfg.Section("default")
	if sec.HasKey("verda_registry_expires_at") {
		t.Errorf("zero ExpiresAt should omit the key, got %q", sec.Key("verda_registry_expires_at").String())
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
