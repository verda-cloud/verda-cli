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
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// TestConfigureFlagMode_DefaultsEndpointAndRegion verifies that supplying only
// the keys runs non-interactively and fills in the default endpoint + region.
func TestConfigureFlagMode_DefaultsEndpointAndRegion(t *testing.T) {
	// No t.Parallel: t.Setenv.
	withTempVerdaHome(t)
	path := filepath.Join(t.TempDir(), "credentials")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", path)

	f := cmdutil.NewTestFactory(nil)
	cmd := NewCmdConfigure(f, cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}})
	cmd.SetArgs([]string{"--access-key", "AKIA", "--secret-key", "secret"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("configure: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	sec := cfg.Section("default")
	if got := sec.Key("verda_s3_endpoint").String(); got != DefaultEndpoint {
		t.Errorf("endpoint = %q, want default %q", got, DefaultEndpoint)
	}
	if got := sec.Key("verda_s3_region").String(); got != defaultRegion {
		t.Errorf("region = %q, want default %q", got, defaultRegion)
	}
}

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

func TestProfileChoices(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credentials")
	content := `[default]
verda_s3_access_key = AKIA
verda_s3_secret_key = secret
verda_s3_endpoint = https://objects.example.storage

[production]
verda_client_id = api-only
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	choices := profileChoices(path)
	if len(choices) != 3 {
		t.Fatalf("choices = %d, want 3 (default, production, create-new): %+v", len(choices), choices)
	}
	if choices[0].Value != "default" || choices[0].Description != "S3 configured" {
		t.Errorf("choice[0] = %+v, want default / S3 configured", choices[0])
	}
	if choices[1].Value != "production" || choices[1].Description != "no S3 credentials yet" {
		t.Errorf("choice[1] = %+v, want production / no S3 credentials yet", choices[1])
	}
	if choices[2].Value != newProfileSentinel {
		t.Errorf("last choice = %+v, want the create-new sentinel", choices[2])
	}
}

func TestProfileChoices_NoFile(t *testing.T) {
	t.Parallel()
	choices := profileChoices(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(choices) != 1 || choices[0].Value != newProfileSentinel {
		t.Errorf("with no credentials file, want only the create-new choice, got %+v", choices)
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
