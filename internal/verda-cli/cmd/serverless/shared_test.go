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

package serverless

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestValidateDeploymentName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string // substring; "" means expect success
	}{
		{"simple", "my-endpoint", ""},
		{"alphanumeric", "api1", ""},
		{"single char", "a", ""},
		{"max length", strings.Repeat("a", 63), ""},
		{"empty", "", "required"},
		{"too long", strings.Repeat("a", 64), "longer than 63"},
		{"uppercase", "My-Endpoint", "lowercase alphanumerics"},
		{"leading hyphen", "-foo", "lowercase alphanumerics"},
		{"trailing hyphen", "foo-", "lowercase alphanumerics"},
		{"underscore", "foo_bar", "lowercase alphanumerics"},
		{"slash", "foo/bar", "lowercase alphanumerics"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDeploymentName(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestRejectLatestTag(t *testing.T) {
	cases := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{"specific tag", "ghcr.io/org/app:v1.2", false},
		{"with digest", "ghcr.io/org/app@sha256:abc", false},
		{"explicit latest", "ghcr.io/org/app:latest", true},
		{"implicit latest", "ghcr.io/org/app", true},
		{"docker hub latest", "nginx:latest", true},
		{"docker hub implicit", "nginx", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectLatestTag(tc.image)
			if tc.wantErr && err == nil {
				t.Fatalf("expected :latest rejection, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}

func TestParseEnvFlag(t *testing.T) {
	cases := []struct {
		name      string
		entry     string
		envType   string
		wantName  string
		wantValue string
		wantErr   string
	}{
		{"plain", "FOO=bar", envTypePlain, "FOO", "bar", ""},
		{"with equals in value", "URL=postgres://u:p@h/db", envTypePlain, "URL", "postgres://u:p@h/db", ""},
		{"secret ref", "TOKEN=my-secret", envTypeSecret, "TOKEN", "my-secret", ""},
		{"empty value OK", "FLAG=", envTypePlain, "FLAG", "", ""},
		{"missing equals", "BAD", envTypePlain, "", "", "expected KEY=VALUE"},
		{"missing name", "=bar", envTypePlain, "", "", "expected KEY=VALUE"},
		{"lowercase name", "foo=bar", envTypePlain, "", "", "invalid env name"},
		{"leading digit", "1FOO=bar", envTypePlain, "", "", "invalid env name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := parseEnvFlag(tc.entry, tc.envType)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.Name != tc.wantName || v.ValueOrReferenceToSecret != tc.wantValue || v.Type != tc.envType {
				t.Fatalf("got %+v, want name=%s value=%s type=%s", v, tc.wantName, tc.wantValue, tc.envType)
			}
		})
	}
}

func TestParseSecretMountFlag(t *testing.T) {
	cases := []struct {
		name       string
		entry      string
		wantSecret string
		wantPath   string
		wantErr    string
	}{
		{"valid", "api-key:/etc/secret/api-key", "api-key", "/etc/secret/api-key", ""},
		{"nested path", "conf:/var/lib/app/conf", "conf", "/var/lib/app/conf", ""},
		{"missing colon", "api-key", "", "", "expected SECRET:MOUNT_PATH"},
		{"empty path", "api-key:", "", "", "expected SECRET:MOUNT_PATH"},
		{"empty secret", ":/etc/foo", "", "", "expected SECRET:MOUNT_PATH"},
		{"relative path", "api-key:etc/foo", "", "", "must be absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := parseSecretMountFlag(tc.entry)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.SecretName != tc.wantSecret || m.MountPath != tc.wantPath || m.Type != mountTypeSecret {
				t.Fatalf("got %+v, want secret=%s path=%s", m, tc.wantSecret, tc.wantPath)
			}
		})
	}
}

func TestDeleteTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cliTimeout time.Duration
		explicit   bool
		timeoutMs  int
		want       time.Duration
	}{
		{"default covers the API wait", 30 * time.Second, false, -1, 75 * time.Second},
		{"explicit --timeout is used as is", 5 * time.Second, true, -1, 5 * time.Second},
		{"explicit --timeout wins over --timeout-ms", 5 * time.Second, true, 120000, 5 * time.Second},
		{"--timeout-ms 0", 30 * time.Second, false, 0, 15 * time.Second},
		{"--timeout-ms above the server cap", 30 * time.Second, false, 900000, 315 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deleteTimeout(tt.cliTimeout, tt.explicit, tt.timeoutMs); got != tt.want {
				t.Errorf("deleteTimeout(%s, %v, %d) = %s, want %s", tt.cliTimeout, tt.explicit, tt.timeoutMs, got, tt.want)
			}
		})
	}
}

func TestStatusColor(t *testing.T) {
	cases := []struct {
		name     string
		status   string
		wantANSI string
		wantBold bool
	}{
		{"running", "running", "2", true},
		{"active", "active", "2", true},
		{"healthy", "healthy", "2", true},
		{"mixed case", "Healthy Running", "2", true},
		{"error", "error", "1", true},
		{"failed", "deploy failed", "1", true},
		{"paused", "paused", "8", false},
		{"stopped", "stopped", "8", false},
		{"offline", "offline", "8", false},
		{"transitional", "initializing", "3", false},
		{"deploying", "deploying", "3", false},
		{"empty", "", "3", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style := statusColor(tc.status)
			want := lipgloss.Color(tc.wantANSI)
			got := style.GetForeground()
			if !colorEqual(got, want) {
				t.Errorf("foreground: got %v, want ANSI %s", got, tc.wantANSI)
			}
			if style.GetBold() != tc.wantBold {
				t.Errorf("bold: got %v, want %v", style.GetBold(), tc.wantBold)
			}
		})
	}
}

func colorEqual(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

func TestCheckMax(t *testing.T) {
	t.Parallel()
	if err := checkMax("replicas", 5, 10); err != nil {
		t.Fatalf("5 <= 10 should pass, got %v", err)
	}
	if err := checkMax("replicas", 10, 10); err != nil {
		t.Fatalf("10 <= 10 should pass, got %v", err)
	}
	err := checkMax("replicas", 11, 10)
	if err == nil {
		t.Fatal("11 > 10 should fail")
	}
	if !strings.Contains(err.Error(), "must be <= 10") {
		t.Errorf("error = %v, want 'must be <= 10'", err)
	}
}

func TestCheckDurationRange(t *testing.T) {
	t.Parallel()
	if err := checkDurationRange("delay", 30*time.Second, time.Hour); err != nil {
		t.Fatalf("30s in 0..1h should pass, got %v", err)
	}
	if err := checkDurationRange("delay", 0, time.Hour); err != nil {
		t.Fatalf("0 should pass, got %v", err)
	}
	err := checkDurationRange("delay", -1*time.Second, time.Hour)
	if err == nil {
		t.Fatal("negative should fail")
	}
	if !strings.Contains(err.Error(), "must be between 0s") {
		t.Errorf("error = %v, want 'must be between 0s'", err)
	}
	err = checkDurationRange("delay", 2*time.Hour, time.Hour)
	if err == nil {
		t.Fatal("over max should fail")
	}
}

func TestCheckUtilThreshold(t *testing.T) {
	t.Parallel()
	if err := checkUtilThreshold("cpu-util", 0); err != nil {
		t.Fatalf("0 (off) should pass, got %v", err)
	}
	if err := checkUtilThreshold("cpu-util", 50); err != nil {
		t.Fatalf("50 should pass, got %v", err)
	}
	err := checkUtilThreshold("cpu-util", 3)
	if err == nil {
		t.Fatal("3 (below minUtilThreshold) should fail")
	}
	if !strings.Contains(err.Error(), "must be 0 (off)") {
		t.Errorf("error = %v, want 'must be 0 (off)'", err)
	}
}

func TestShortDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, "1h"},
		{30 * time.Minute, "30m"},
		{90 * time.Second, "1m30s"},
		{5 * time.Second, "5s"},
		{2*time.Hour + 30*time.Minute, "2h30m"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := shortDuration(tt.d); got != tt.want {
				t.Errorf("shortDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestInteractiveTTY(t *testing.T) {
	orig := isInteractiveTerminal
	t.Cleanup(func() { isInteractiveTerminal = orig })

	isInteractiveTerminal = func() bool { return true }
	f := &cmdutil.TestFactory{OutputFormatOverride: "table"}
	if !interactiveTTY(f) {
		t.Error("expected true for terminal+table+non-agent")
	}

	f2 := &cmdutil.TestFactory{AgentModeOverride: true, OutputFormatOverride: "table"}
	if interactiveTTY(f2) {
		t.Error("expected false for agent mode")
	}

	f3 := &cmdutil.TestFactory{OutputFormatOverride: "json"}
	if interactiveTTY(f3) {
		t.Error("expected false for json output")
	}

	isInteractiveTerminal = func() bool { return false }
	f4 := &cmdutil.TestFactory{OutputFormatOverride: "table"}
	if interactiveTTY(f4) {
		t.Error("expected false for non-terminal")
	}
}
