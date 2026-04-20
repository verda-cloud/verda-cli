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
	"context"
	"errors"
	"os"
	"path/filepath"
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
// the bug where `verda s3 ls` reported "no S3 credentials configured" even
// after `verda s3 configure` saved them to the [default] profile. S3 commands
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
