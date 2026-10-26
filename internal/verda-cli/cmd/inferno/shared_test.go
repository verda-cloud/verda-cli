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

package inferno

import (
	"strings"
	"testing"
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

func TestBuildEnvVars(t *testing.T) {
	t.Run("nil inputs", func(t *testing.T) {
		out, err := buildEnvVars(nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != nil {
			t.Fatalf("expected nil, got %+v", out)
		}
	})

	t.Run("mixed plain and secret", func(t *testing.T) {
		plain := []string{"HF_HOME=/data/.hf", "DEBUG=1"}
		secret := []string{"TOKEN=my-secret"}
		out, err := buildEnvVars(plain, secret)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 3 {
			t.Fatalf("expected 3 env vars, got %d", len(out))
		}
		wantTypes := []string{envTypePlain, envTypePlain, envTypeSecret}
		for i, want := range wantTypes {
			if out[i].Type != want {
				t.Errorf("env[%d] type: got %q, want %q", i, out[i].Type, want)
			}
		}
		if out[0].Name != "HF_HOME" || out[0].ValueOrReferenceToSecret != "/data/.hf" {
			t.Errorf("env[0]: got %+v", out[0])
		}
	})

	t.Run("invalid plain entry", func(t *testing.T) {
		_, err := buildEnvVars([]string{"bad"}, nil)
		if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
			t.Fatalf("expected KEY=VALUE error, got %v", err)
		}
	})

	t.Run("invalid secret entry", func(t *testing.T) {
		_, err := buildEnvVars(nil, []string{"bad"})
		if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
			t.Fatalf("expected KEY=VALUE error, got %v", err)
		}
	})
}

func TestBuildVolumeMounts(t *testing.T) {
	t.Run("no secret mounts", func(t *testing.T) {
		mounts, err := buildVolumeMounts(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mounts) != 1 {
			t.Fatalf("expected 1 mount (scratch /data), got %d", len(mounts))
		}
		if mounts[0].Type != mountTypeScratch || mounts[0].MountPath != defaultGeneralStoragePath {
			t.Errorf("expected scratch at /data, got %+v", mounts[0])
		}
	})

	t.Run("with secret mounts", func(t *testing.T) {
		mounts, err := buildVolumeMounts([]string{"my-secret:/etc/creds/token"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mounts) != 2 {
			t.Fatalf("expected 2 mounts, got %d", len(mounts))
		}
		if mounts[0].Type != mountTypeScratch {
			t.Errorf("first mount should be scratch, got %+v", mounts[0])
		}
		if mounts[1].Type != mountTypeSecret || mounts[1].SecretName != "my-secret" || mounts[1].MountPath != "/etc/creds/token" {
			t.Errorf("second mount should be secret, got %+v", mounts[1])
		}
	})

	t.Run("invalid secret mount", func(t *testing.T) {
		_, err := buildVolumeMounts([]string{"no-colon"})
		if err == nil || !strings.Contains(err.Error(), "expected SECRET:MOUNT_PATH") {
			t.Fatalf("expected error, got %v", err)
		}
	})
}

func TestResolveQueueLoad(t *testing.T) {
	cases := []struct {
		name      string
		preset    string
		custom    int
		wantLoad  int
		expectErr bool
	}{
		{"instant", presetInstant, 0, queueLoadInstant, false},
		{"balanced", presetBalanced, 0, queueLoadBalanced, false},
		{"cost-saver", presetCostSaver, 0, queueLoadCostSaver, false},
		{"cost_saver alias", "cost_saver", 0, queueLoadCostSaver, false},
		{"costsaver alias", "costsaver", 0, queueLoadCostSaver, false},
		{"empty defaults to balanced", "", 0, queueLoadBalanced, false},
		{"custom with value", presetCustom, 42, 42, false},
		{"queue-load alone implies custom", "", 17, 17, false},
		{"custom without value", presetCustom, 0, 0, true},
		{"unknown preset", "unknown", 0, 0, true},
		{"custom over 1000", presetCustom, 1001, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveQueueLoad(tc.preset, tc.custom)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantLoad {
				t.Errorf("preset %q custom %d → got %d, want %d", tc.preset, tc.custom, got, tc.wantLoad)
			}
		})
	}
}
