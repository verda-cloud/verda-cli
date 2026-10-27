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
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestComputeFromCurrent_Nil(t *testing.T) {
	got := computeFromCurrent(nil)
	if got.Name != "" || got.Size != 0 {
		t.Fatalf("expected zero value, got %+v", got)
	}
}

func TestComputeFromCurrent_NonNil(t *testing.T) {
	got := computeFromCurrent(&verda.ContainerCompute{Name: "RTX4500Ada", Size: 2})
	if got.Name != "RTX4500Ada" || got.Size != 2 {
		t.Fatalf("expected RTX4500Ada x2, got %+v", got)
	}
}

func TestAnyFlagChanged(t *testing.T) {
	cases := []struct {
		name    string
		setArgs []string
		check   []string
		want    bool
	}{
		{"none set", nil, []string{"image", "port"}, false},
		{"one matching", []string{"--image", "x"}, []string{"image", "port"}, true},
		{"other matching", []string{"--port", "80"}, []string{"image", "port"}, true},
		{"both matching", []string{"--image", "x", "--port", "80"}, []string{"image", "port"}, true},
		{"set but not checked", []string{"--image", "x"}, []string{"port"}, false},
		{"empty check list", []string{"--image", "x"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("image", "", "")
			cmd.Flags().Int("port", 0, "")
			if err := cmd.ParseFlags(tc.setArgs); err != nil {
				t.Fatalf("parse flags: %v", err)
			}
			got := anyFlagChanged(cmd, tc.check...)
			if got != tc.want {
				t.Fatalf("anyFlagChanged = %v, want %v", got, tc.want)
			}
		})
	}
}

func envMergeCurrent() []verda.ContainerEnvVar {
	return []verda.ContainerEnvVar{
		{Name: "FOO", ValueOrReferenceToSecret: "bar", Type: envTypePlain},
		{Name: "BAZ", ValueOrReferenceToSecret: "qux", Type: envTypePlain},
		{Name: "SECRET_KEY", ValueOrReferenceToSecret: "my-secret", Type: envTypeSecret},
		{Name: "API_TOKEN", ValueOrReferenceToSecret: "token-secret", Type: envTypeSecret},
	}
}

func TestMergeEnvVarsForUpdate_NeitherChanged(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(false, false, nil, nil, envMergeCurrent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 env vars (all carried), got %d: %+v", len(got), got)
	}
}

func TestMergeEnvVarsForUpdate_PlainChangedCarriesSecret(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(true, false, []string{"NEW=val"}, nil, envMergeCurrent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 (1 new plain + 2 carried secrets), got %d: %+v", len(got), got)
	}
	if got[0].Name != "NEW" || got[0].Type != envTypePlain {
		t.Errorf("first should be NEW (plain), got %+v", got[0])
	}
	if got[1].Name != "SECRET_KEY" || got[1].Type != envTypeSecret {
		t.Errorf("second should be SECRET_KEY (carried secret), got %+v", got[1])
	}
	if got[2].Name != "API_TOKEN" || got[2].Type != envTypeSecret {
		t.Errorf("third should be API_TOKEN (carried secret), got %+v", got[2])
	}
}

func TestMergeEnvVarsForUpdate_SecretChangedCarriesPlain(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(false, true, nil, []string{"NEW_SEC=new-secret"}, envMergeCurrent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 (2 carried plain + 1 new secret), got %d: %+v", len(got), got)
	}
	if got[0].Name != "FOO" || got[0].Type != envTypePlain {
		t.Errorf("first should be FOO (carried plain), got %+v", got[0])
	}
	if got[1].Name != "BAZ" || got[1].Type != envTypePlain {
		t.Errorf("second should be BAZ (carried plain), got %+v", got[1])
	}
	if got[2].Name != "NEW_SEC" || got[2].Type != envTypeSecret {
		t.Errorf("third should be NEW_SEC (secret), got %+v", got[2])
	}
}

func TestMergeEnvVarsForUpdate_BothChanged(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(true, true, []string{"A=1"}, []string{"B=secret-b"}, envMergeCurrent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 (1 plain + 1 secret from flags), got %d: %+v", len(got), got)
	}
	if got[0].Name != "A" || got[0].Type != envTypePlain {
		t.Errorf("first should be A (plain), got %+v", got[0])
	}
	if got[1].Name != "B" || got[1].Type != envTypeSecret {
		t.Errorf("second should be B (secret), got %+v", got[1])
	}
}

func TestMergeEnvVarsForUpdate_ClearPlainKeepsSecret(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(true, false, nil, nil, envMergeCurrent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 (0 plain + 2 carried secrets), got %d: %+v", len(got), got)
	}
	for _, e := range got {
		if e.Type != envTypeSecret {
			t.Errorf("expected only secrets, got %+v", e)
		}
	}
}

func TestMergeEnvVarsForUpdate_BadPlainFormat(t *testing.T) {
	_, err := mergeEnvVarsForUpdate(true, false, []string{"BADFORMAT"}, nil, envMergeCurrent())
	if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestMergeEnvVarsForUpdate_BadSecretFormat(t *testing.T) {
	_, err := mergeEnvVarsForUpdate(false, true, nil, []string{"nope"}, envMergeCurrent())
	if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestMergeEnvVarsForUpdate_NilCurrent(t *testing.T) {
	got, err := mergeEnvVarsForUpdate(false, false, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 env vars, got %d", len(got))
	}
}

func TestMergeVolumeMountsForUpdate(t *testing.T) {
	current := []verda.ContainerVolumeMount{
		{Type: mountTypeScratch, MountPath: "/data"},
		{Type: mountTypeSecret, MountPath: "/etc/old-secret", SecretName: "old-secret"},
	}

	t.Run("replaces secrets keeps scratch", func(t *testing.T) {
		got, err := mergeVolumeMountsForUpdate(
			[]string{"new-secret:/etc/new-secret"},
			current,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 mounts (1 scratch + 1 new secret), got %d: %+v", len(got), got)
		}
		if got[0].Type != mountTypeScratch || got[0].MountPath != "/data" {
			t.Errorf("first mount should be scratch /data, got %+v", got[0])
		}
		if got[1].Type != mountTypeSecret || got[1].SecretName != "new-secret" || got[1].MountPath != "/etc/new-secret" {
			t.Errorf("second mount should be new-secret, got %+v", got[1])
		}
	})

	t.Run("empty secrets removes all secret mounts", func(t *testing.T) {
		got, err := mergeVolumeMountsForUpdate(nil, current)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 mount (scratch only), got %d: %+v", len(got), got)
		}
		if got[0].Type != mountTypeScratch {
			t.Errorf("expected scratch, got %+v", got[0])
		}
	})

	t.Run("multiple new secrets", func(t *testing.T) {
		got, err := mergeVolumeMountsForUpdate(
			[]string{"s1:/etc/s1", "s2:/etc/s2"},
			current,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 mounts (1 scratch + 2 secrets), got %d", len(got))
		}
	})

	t.Run("nil current with new secrets", func(t *testing.T) {
		got, err := mergeVolumeMountsForUpdate(
			[]string{"s1:/etc/s1"},
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].SecretName != "s1" {
			t.Fatalf("expected 1 secret mount, got %+v", got)
		}
	})

	t.Run("error on bad format", func(t *testing.T) {
		_, err := mergeVolumeMountsForUpdate([]string{"bad-no-colon"}, current)
		if err == nil || !strings.Contains(err.Error(), "expected SECRET:MOUNT_PATH") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})

	t.Run("error on relative path", func(t *testing.T) {
		_, err := mergeVolumeMountsForUpdate([]string{"s1:relative/path"}, current)
		if err == nil || !strings.Contains(err.Error(), "must be absolute") {
			t.Fatalf("expected absolute path error, got %v", err)
		}
	})
}

func TestMergeEntrypointForUpdate_EntrypointOnly(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/old"},
		Cmd:        []string{"serve"},
	}
	got := mergeEntrypointForUpdate(true, false, []string{"/new"}, nil, current)
	if got == nil || !got.Enabled {
		t.Fatal("expected non-nil enabled result")
	}
	if len(got.Entrypoint) != 1 || got.Entrypoint[0] != "/new" {
		t.Errorf("entrypoint: got %v, want [/new]", got.Entrypoint)
	}
	if len(got.Cmd) != 1 || got.Cmd[0] != "serve" {
		t.Errorf("cmd: got %v, want [serve] (carried from current)", got.Cmd)
	}
}

func TestMergeEntrypointForUpdate_CmdOnly(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"old-cmd"},
	}
	got := mergeEntrypointForUpdate(false, true, nil, []string{"new-cmd", "--flag"}, current)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if len(got.Entrypoint) != 1 || got.Entrypoint[0] != "/bin/sh" {
		t.Errorf("entrypoint: got %v, want [/bin/sh] (carried)", got.Entrypoint)
	}
	if len(got.Cmd) != 2 || got.Cmd[0] != "new-cmd" {
		t.Errorf("cmd: got %v, want [new-cmd --flag]", got.Cmd)
	}
}

func TestMergeEntrypointForUpdate_BothChanged(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/old"},
		Cmd:        []string{"old"},
	}
	got := mergeEntrypointForUpdate(true, true, []string{"/new"}, []string{"new"}, current)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Entrypoint[0] != "/new" || got.Cmd[0] != "new" {
		t.Errorf("got ep=%v cmd=%v", got.Entrypoint, got.Cmd)
	}
}

func TestMergeEntrypointForUpdate_NilCurrentNeitherChanged(t *testing.T) {
	got := mergeEntrypointForUpdate(false, false, nil, nil, nil)
	if got != nil {
		t.Fatalf("expected nil (no overrides), got %+v", got)
	}
}

func TestMergeEntrypointForUpdate_CarriesCurrent(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/app"},
		Cmd:        []string{"start"},
	}
	got := mergeEntrypointForUpdate(false, false, nil, nil, current)
	if got == nil {
		t.Fatal("expected non-nil (carried from current)")
	}
	if got.Entrypoint[0] != "/app" || got.Cmd[0] != "start" {
		t.Errorf("got %+v, want carried values", got)
	}
}

func TestMergeEntrypointForUpdate_ClearBothReturnsNil(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/old"},
		Cmd:        []string{"old"},
	}
	got := mergeEntrypointForUpdate(true, true, nil, nil, current)
	if got != nil {
		t.Fatalf("expected nil when both cleared, got %+v", got)
	}
}

func TestMergeEntrypointForUpdate_ClearEntrypointKeepsCmd(t *testing.T) {
	current := &verda.ContainerEntrypointOverrides{
		Enabled:    true,
		Entrypoint: []string{"/old"},
		Cmd:        []string{"serve"},
	}
	got := mergeEntrypointForUpdate(true, false, nil, nil, current)
	if got == nil {
		t.Fatal("expected non-nil (cmd carried)")
	}
	if len(got.Entrypoint) != 0 {
		t.Errorf("entrypoint should be cleared, got %v", got.Entrypoint)
	}
	if got.Cmd[0] != "serve" {
		t.Errorf("cmd should be carried, got %v", got.Cmd)
	}
}

func TestMergeEntrypointForUpdate_NilCurrentNewEntrypoint(t *testing.T) {
	got := mergeEntrypointForUpdate(true, false, []string{"/app"}, nil, nil)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if !got.Enabled || got.Entrypoint[0] != "/app" {
		t.Errorf("got %+v", got)
	}
}
