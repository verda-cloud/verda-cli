package options

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoadSharedCredentials(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
verda_client_id = default-id
verda_client_secret = default-secret

[dev]
verda_client_id = dev-id
verda_client_secret = dev-secret
verda_token = dev-token
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
verda_client_id = default-id
verda_client_secret = default-secret
verda_token = default-token
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
verda_client_id = shared-id
verda_client_secret = shared-secret
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

func TestOptionsValidateOutputFormat(t *testing.T) {
	t.Parallel()

	valid := []string{"table", "json", "yaml"}
	for _, format := range valid {
		opts := &Options{
			Server:      "https://api.verda.com/v1",
			Timeout:     30,
			Output:      format,
			AuthOptions: &AuthOptions{},
		}
		if err := opts.Validate(); err != nil {
			t.Errorf("Validate() with output=%q returned error: %v", format, err)
		}
	}
}

func TestOptionsValidateRejectsInvalidOutput(t *testing.T) {
	t.Parallel()

	opts := &Options{
		Server:      "https://api.verda.com/v1",
		Timeout:     30,
		Output:      "xml",
		AuthOptions: &AuthOptions{},
	}

	err := opts.Validate()
	if err == nil {
		t.Fatal("expected Validate() to reject output=xml")
	}
	if !strings.Contains(err.Error(), "xml") {
		t.Fatalf("expected error to mention 'xml', got: %v", err)
	}
}

func TestOptionsCompleteDefaultsOutputToTable(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
verda_client_id = id
verda_client_secret = secret
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

	if opts.Output != "table" {
		t.Fatalf("expected Output to default to 'table', got %q", opts.Output)
	}
}

func TestOptionsCompleteKeepsExplicitOutput(t *testing.T) {
	t.Parallel()

	path := writeCredentialsFile(t, `
[default]
verda_client_id = id
verda_client_secret = secret
`)

	opts := &Options{
		Server:  "https://api.verda.com/v1",
		Timeout: 30,
		Output:  "json",
		AuthOptions: &AuthOptions{
			CredentialsFile: path,
			Profile:         "default",
		},
	}

	opts.Complete()

	if opts.Output != "json" {
		t.Fatalf("expected Output to remain 'json', got %q", opts.Output)
	}
}

// ---------------------------------------------------------------------------
// Credential resolution priority tests
//
// Auto-resolved profile (no explicit --auth.profile):
//   1. CLI flags          (--auth.client-id=xxx)
//   2. Config file        (viper: auth.client-id in config.yaml)
//   3. Environment vars   (VERDA_CLIENT_ID)
//   4. Credentials file   ([default] section in ~/.verda/credentials)
//
// Explicit profile (--auth.profile=staging):
//   1. CLI flags          — always wins
//   2. Credentials file   — explicit profile promotes its creds
//   3. Config file        — viper values
//   4. Environment vars   — lowest
//
// These tests are NOT parallel because they mutate global viper state.
// ---------------------------------------------------------------------------

// credSource describes which sources to populate in a test case.
type credSource struct {
	flag  string // simulate --auth.client-id / --auth.client-secret
	viper string // simulate config file value
	env   string // simulate VERDA_CLIENT_ID / VERDA_CLIENT_SECRET
	cred  string // value in credentials file
}

func TestCredentialPriority(t *testing.T) {
	tests := []struct {
		name    string
		profile string // empty = auto-resolve, non-empty = explicit
		id      credSource
		secret  credSource
		wantID  string
		wantSec string
	}{
		// --- Auto-resolved profile: flag > viper > env > creds ---
		{
			name:    "all sources set, flag wins",
			id:      credSource{flag: "flag-id", viper: "viper-id", env: "env-id", cred: "cred-id"},
			secret:  credSource{flag: "flag-secret", viper: "viper-secret", env: "env-secret", cred: "cred-secret"},
			wantID:  "flag-id",
			wantSec: "flag-secret",
		},
		{
			name:    "no flag, viper wins over env and creds",
			id:      credSource{viper: "viper-id", env: "env-id", cred: "cred-id"},
			secret:  credSource{viper: "viper-secret", env: "env-secret", cred: "cred-secret"},
			wantID:  "viper-id",
			wantSec: "viper-secret",
		},
		{
			name:    "no flag or viper, env wins over creds",
			id:      credSource{env: "env-id", cred: "cred-id"},
			secret:  credSource{env: "env-secret", cred: "cred-secret"},
			wantID:  "env-id",
			wantSec: "env-secret",
		},
		{
			name:    "only creds file, used as fallback",
			id:      credSource{cred: "cred-id"},
			secret:  credSource{cred: "cred-secret"},
			wantID:  "cred-id",
			wantSec: "cred-secret",
		},
		{
			name:    "mixed: ID from flag, secret from env",
			id:      credSource{flag: "flag-id", cred: "cred-id"},
			secret:  credSource{env: "env-secret", cred: "cred-secret"},
			wantID:  "flag-id",
			wantSec: "env-secret",
		},
		{
			name:    "mixed: ID from viper, secret from creds",
			id:      credSource{viper: "viper-id", cred: "cred-id"},
			secret:  credSource{cred: "cred-secret"},
			wantID:  "viper-id",
			wantSec: "cred-secret",
		},

		// --- Explicit profile: flag > creds > viper > env ---
		{
			name:    "explicit profile: creds override viper and env",
			profile: "staging",
			id:      credSource{viper: "viper-id", env: "env-id", cred: "staging-id"},
			secret:  credSource{viper: "viper-secret", env: "env-secret", cred: "staging-secret"},
			wantID:  "staging-id",
			wantSec: "staging-secret",
		},
		{
			name:    "explicit profile: flag still wins over creds",
			profile: "staging",
			id:      credSource{flag: "flag-id", cred: "staging-id"},
			secret:  credSource{cred: "staging-secret"},
			wantID:  "flag-id",
			wantSec: "staging-secret",
		},
		{
			name:    "explicit profile: mixed flag + creds + env",
			profile: "staging",
			id:      credSource{flag: "flag-id", env: "env-id", cred: "staging-id"},
			secret:  credSource{env: "env-secret", cred: "staging-secret"},
			wantID:  "flag-id",
			wantSec: "staging-secret", // explicit profile creds beat env
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// --- credentials file ---
			profile := tt.profile
			if profile == "" {
				profile = "default"
			}
			creds := "[" + profile + "]\n"
			if tt.id.cred != "" {
				creds += "verda_client_id = " + tt.id.cred + "\n"
			}
			if tt.secret.cred != "" {
				creds += "verda_client_secret = " + tt.secret.cred + "\n"
			}
			path := writeCredentialsFile(t, creds)

			// --- viper (config file) ---
			if tt.id.viper != "" {
				viper.Set("auth.client-id", tt.id.viper)
			}
			if tt.secret.viper != "" {
				viper.Set("auth.client-secret", tt.secret.viper)
			}
			t.Cleanup(func() {
				viper.Set("auth.client-id", "")
				viper.Set("auth.client-secret", "")
			})

			// --- environment variables ---
			if tt.id.env != "" {
				t.Setenv("VERDA_CLIENT_ID", tt.id.env)
			}
			if tt.secret.env != "" {
				t.Setenv("VERDA_CLIENT_SECRET", tt.secret.env)
			}

			// --- build options ---
			opts := &Options{
				Server:  defaultBaseURL,
				Timeout: 30,
				AuthOptions: &AuthOptions{
					ClientID:        tt.id.flag,
					ClientSecret:    tt.secret.flag,
					Profile:         tt.profile,
					CredentialsFile: path,
				},
			}

			opts.Complete()

			if opts.AuthOptions.ClientID != tt.wantID {
				t.Errorf("ClientID: want %q, got %q", tt.wantID, opts.AuthOptions.ClientID)
			}
			if opts.AuthOptions.ClientSecret != tt.wantSec {
				t.Errorf("ClientSecret: want %q, got %q", tt.wantSec, opts.AuthOptions.ClientSecret)
			}
		})
	}
}
