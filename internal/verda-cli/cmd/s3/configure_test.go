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
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

func TestResolveCredentialsFileUsesEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", path)

	got, err := resolveCredentialsFile("")
	if err != nil {
		t.Fatalf("resolveCredentialsFile() error: %v", err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func TestResolveCredentialsFileExplicit(t *testing.T) {
	t.Parallel()

	got, err := resolveCredentialsFile("/custom/path")
	if err != nil {
		t.Fatalf("resolveCredentialsFile() error: %v", err)
	}
	if got != "/custom/path" {
		t.Fatalf("got %q, want %q", got, "/custom/path")
	}
}

func TestConfigureWritesINI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	// Pre-populate with existing API credentials.
	existing := `[default]
verda_client_id = my-api-id
verda_client_secret = my-api-secret
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simulate what the configure command does: load, add S3 keys, save.
	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	section, err := cfg.GetSection("default")
	if err != nil {
		t.Fatal(err)
	}

	section.Key("verda_s3_access_key").SetValue("AKIA123")
	section.Key("verda_s3_secret_key").SetValue("secret456")
	section.Key("verda_s3_endpoint").SetValue("https://objects.lab.verda.storage")
	section.Key("verda_s3_region").SetValue("us-east-1")

	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}

	// Verify API credentials are preserved.
	cfg2, err := ini.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	sec := cfg2.Section("default")
	if sec.Key("verda_client_id").String() != "my-api-id" {
		t.Error("API client_id was clobbered")
	}
	if sec.Key("verda_s3_access_key").String() != "AKIA123" {
		t.Error("S3 access_key not written")
	}
	if sec.Key("verda_s3_endpoint").String() != "https://objects.lab.verda.storage" {
		t.Error("S3 endpoint not written")
	}
}
