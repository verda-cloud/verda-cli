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

package objectstorage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

func TestBuildClientUsesSwap(t *testing.T) {
	// Do NOT use t.Parallel — we're mutating a package-level var.

	called := false
	orig := clientBuilder
	t.Cleanup(func() { clientBuilder = orig })

	clientBuilder = func(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (API, error) {
		called = true
		return nil, errors.New("fake")
	}

	_, err := buildClient(context.Background(), nil, ClientOverrides{})
	if err == nil || err.Error() != "fake" {
		t.Fatalf("expected fake error, got %v", err)
	}
	if !called {
		t.Fatal("swapped builder not invoked")
	}
}

// TestLoadCredsFromFactoryFallsBackToDefaultProfile is a regression test for
// the bug where `verda object-storage ls` reported "no S3 credentials configured" even
// after `verda object-storage configure` saved them to the [default] profile. S3 commands
// skip Options.Complete(), so AuthOptions.Profile stays empty — passing ""
// to ini.GetSection loaded the synthetic DEFAULT section (with no keys)
// instead of the user's [default] section.
func TestLoadCredsFromFactoryFallsBackToDefaultProfile(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	contents := "[default]\n" +
		"verda_s3_access_key = AKIA_TEST\n" +
		"verda_s3_secret_key = SECRET_TEST\n" +
		"verda_s3_endpoint   = https://example.invalid\n" +
		"verda_s3_region     = us-east-1\n"
	if err := os.WriteFile(credsPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", credsPath)

	// AuthOptions.Profile left empty — mirrors what s3 commands see in
	// production because they're in skipCredentialResolution.
	f := &cmdutil.TestFactory{
		OptionsOverride: &options.Options{
			AuthOptions: &options.AuthOptions{},
		},
	}

	creds, err := loadCredsFromFactory(f)
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.AccessKey != "AKIA_TEST" {
		t.Errorf("AccessKey = %q, want AKIA_TEST (empty means we hit the synthetic DEFAULT section)", creds.AccessKey)
	}
	if creds.SecretKey != "SECRET_TEST" {
		t.Errorf("SecretKey = %q, want SECRET_TEST", creds.SecretKey)
	}
	if creds.Endpoint != "https://example.invalid" {
		t.Errorf("Endpoint = %q, want https://example.invalid", creds.Endpoint)
	}
}

// s3TestFactory is the shape every S3 command sees in production: S3 commands
// are in skipCredentialResolution, so AuthOptions.Profile is never resolved.
func s3TestFactory() *cmdutil.TestFactory {
	return &cmdutil.TestFactory{
		OptionsOverride: &options.Options{
			AuthOptions: &options.AuthOptions{},
		},
	}
}

func writeS3Profile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	return path
}

// CI has no credentials file and must not have to write secrets to disk:
// VERDA_S3_* alone has to be enough. Before this fix nothing in the tree ever
// read those variables, so a complete env resolved to "no S3 credentials".
func TestLoadCredsFromFactoryHonorsEnvWithoutFile(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("VERDA_PROFILE", "default")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")
	t.Setenv("VERDA_S3_SECRET_KEY", "REPLACE_ME_ENV_SECRET")
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")
	t.Setenv("VERDA_S3_REGION", "eu-north-1")

	creds, err := loadCredsFromFactory(s3TestFactory())
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if !creds.HasCredentials() {
		t.Fatalf("HasCredentials() = false; env-only credentials were ignored: %+v", redactedCreds(creds))
	}
	if creds.AccessKey != "REPLACE_ME_ENV_KEY" || creds.SecretKey != "REPLACE_ME_ENV_SECRET" {
		t.Errorf("key material not taken from env: %+v", redactedCreds(creds))
	}
	if creds.Endpoint != "https://env.example.invalid" {
		t.Errorf("Endpoint = %q, want the env value", creds.Endpoint)
	}
	if creds.Region != "eu-north-1" {
		t.Errorf("Region = %q, want eu-north-1", creds.Region)
	}
}

// The decided semantics: per-field merge, env above file. One env var must not
// discard the rest of a working profile.
func TestLoadCredsFromFactoryEnvOverridesFilePerField(t *testing.T) {
	path := writeS3Profile(t, "[default]\n"+
		"verda_s3_access_key = FILE_KEY\n"+
		"verda_s3_secret_key = FILE_SECRET\n"+
		"verda_s3_endpoint   = https://file.example.invalid\n"+
		"verda_s3_region     = us-east-1\n")

	t.Setenv("VERDA_PROFILE", "default")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")

	creds, err := loadCredsFromFactory(s3TestFactory())
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.Endpoint != "https://env.example.invalid" {
		t.Errorf("Endpoint = %q, want the env override", creds.Endpoint)
	}
	if creds.AccessKey != "FILE_KEY" || creds.SecretKey != "FILE_SECRET" {
		t.Errorf("env endpoint wiped file key material: %+v", redactedCreds(creds))
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q, want the file value us-east-1", creds.Region)
	}
}

// An exported-but-empty variable (`export VERDA_S3_REGION=`) means unset, not
// "blank the profile".
func TestLoadCredsFromFactoryEmptyEnvKeepsFileValue(t *testing.T) {
	path := writeS3Profile(t, "[default]\n"+
		"verda_s3_access_key = FILE_KEY\n"+
		"verda_s3_secret_key = FILE_SECRET\n"+
		"verda_s3_endpoint   = https://file.example.invalid\n"+
		"verda_s3_region     = us-east-1\n")

	t.Setenv("VERDA_PROFILE", "default")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_S3_REGION", "")

	creds, err := loadCredsFromFactory(s3TestFactory())
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.Region != "us-east-1" {
		t.Errorf("Region = %q; an empty env var blanked the file value", creds.Region)
	}
}

// A partial env set with no file must stay incomplete — NewClient then produces
// the "run configure" hint rather than half-authenticating.
func TestLoadCredsFromFactoryPartialEnvStaysIncomplete(t *testing.T) {
	t.Setenv("VERDA_PROFILE", "default")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")

	creds, err := loadCredsFromFactory(s3TestFactory())
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.HasCredentials() {
		t.Errorf("HasCredentials() = true with only an access key set: %+v", redactedCreds(creds))
	}

	_, err = NewClient(context.Background(), creds, creds.AuthMode, ClientOverrides{})
	if err == nil {
		t.Fatal("NewClient succeeded on incomplete credentials")
	}
	if strings.Contains(err.Error(), "REPLACE_ME_ENV_KEY") {
		t.Errorf("error message leaks key material: %v", err)
	}
}

// Flags still outrank env — env sits between flags and file.
func TestClientOverridesBeatEnv(t *testing.T) {
	t.Setenv("VERDA_PROFILE", "default")
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("VERDA_S3_ACCESS_KEY", "REPLACE_ME_ENV_KEY")
	t.Setenv("VERDA_S3_SECRET_KEY", "REPLACE_ME_ENV_SECRET")
	t.Setenv("VERDA_S3_ENDPOINT", "https://env.example.invalid")

	creds, err := loadCredsFromFactory(s3TestFactory())
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if got := resolveEndpoint(creds, "https://flag.example.invalid"); got != "https://flag.example.invalid" {
		t.Errorf("resolveEndpoint = %q, want the flag value to win over env", got)
	}
	if got := resolveEndpoint(creds, ""); got != "https://env.example.invalid" {
		t.Errorf("resolveEndpoint = %q, want the env value when no flag is passed", got)
	}
}

// redactedCreds keeps key material out of test failure output.
func redactedCreds(c *options.S3Credentials) map[string]any {
	return map[string]any{
		"access_key_set": c.AccessKey != "",
		"secret_key_set": c.SecretKey != "",
		"endpoint":       c.Endpoint,
		"region":         c.Region,
		"auth_mode":      c.AuthMode,
	}
}
