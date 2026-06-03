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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// writeCredsFile writes an INI credentials file under t.TempDir and returns its
// path. content is the raw INI body.
func writeCredsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	return path
}

func runShow(t *testing.T, path string, args ...string) (stdout, stderr string) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	f := cmdutil.NewTestFactory(nil)
	// Mirror production wiring: clioptions.New() always sets AuthOptions, which
	// show reads to default the profile. The bare TestFactory leaves it nil.
	f.OptionsOverride = &clioptions.Options{
		Server:      "https://test.verda.com/v1",
		Timeout:     10 * time.Second,
		Output:      "table",
		AuthOptions: &clioptions.AuthOptions{},
	}
	cmd := NewCmdShow(f, cmdutil.IOStreams{Out: out, ErrOut: errOut})
	full := append([]string{"--credentials-file", path}, args...)
	cmd.SetArgs(full)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out.String(), errOut.String()
}

func TestShow_Configured(t *testing.T) {
	// no t.Parallel — NewTestFactory + shared package state kept consistent with siblings
	path := writeCredsFile(t, `[default]
verda_s3_access_key = AKIA123
verda_s3_secret_key = secret456
verda_s3_endpoint = https://objects.example.com
verda_s3_region = eu-north-1
`)
	stdout, stderr := runShow(t, path)

	for _, want := range []string{
		"profile:           default",
		"access_key_loaded: true",
		"secret_key_loaded: true",
		"https://objects.example.com",
		"eu-north-1",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stderr, "Missing") {
		t.Errorf("did not expect a 'Missing' warning for fully-configured creds:\n%s", stderr)
	}
}

// TestShow_PartialMissingEndpoint exercises the HasCredentials()==false branch
// and valueOrDash: with no endpoint/region set, both render as "-" and a
// "Missing" hint is printed to stderr.
func TestShow_PartialMissingEndpoint(t *testing.T) {
	// no t.Parallel
	path := writeCredsFile(t, `[default]
verda_s3_access_key = AKIA123
verda_s3_secret_key = secret456
`)
	stdout, stderr := runShow(t, path)

	if !strings.Contains(stdout, "endpoint:          -") {
		t.Errorf("expected endpoint rendered as '-' (valueOrDash):\n%s", stdout)
	}
	if !strings.Contains(stdout, "region:            -") {
		t.Errorf("expected region rendered as '-' (valueOrDash):\n%s", stdout)
	}
	if !strings.Contains(stderr, "Missing") || !strings.Contains(stderr, "endpoint") {
		t.Errorf("expected a 'Missing: endpoint' hint on stderr:\n%s", stderr)
	}
}

func TestShow_NotConfigured(t *testing.T) {
	// no t.Parallel
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	stdout, stderr := runShow(t, missing)

	if !strings.Contains(stdout, "s3_configured") || !strings.Contains(stdout, "false") {
		t.Errorf("expected 's3_configured: false' for a missing creds file:\n%s", stdout)
	}
	if !strings.Contains(stderr, "No S3 credentials found") {
		t.Errorf("expected guidance on stderr when not configured:\n%s", stderr)
	}
}

func TestShow_CustomProfile(t *testing.T) {
	// no t.Parallel
	path := writeCredsFile(t, `[default]
verda_s3_access_key = default-key
verda_s3_secret_key = default-secret
verda_s3_endpoint = https://default.example.com

[staging]
verda_s3_access_key = staging-key
verda_s3_secret_key = staging-secret
verda_s3_endpoint = https://staging.example.com
verda_s3_region = eu-west-1
`)
	stdout, _ := runShow(t, path, "--profile", "staging")

	if !strings.Contains(stdout, "profile:           staging") {
		t.Errorf("expected the staging profile to be shown:\n%s", stdout)
	}
	if !strings.Contains(stdout, "https://staging.example.com") {
		t.Errorf("expected the staging endpoint, not default's:\n%s", stdout)
	}
	if strings.Contains(stdout, "https://default.example.com") {
		t.Errorf("staging show leaked the default profile's endpoint:\n%s", stdout)
	}
}
