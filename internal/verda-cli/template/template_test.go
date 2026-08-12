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

package template

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func saveAndReload(t *testing.T, tmpl *Template) *Template {
	t.Helper()
	dir := t.TempDir()
	name := "rt"
	if err := Save(dir, tmpl.Resource, name, tmpl); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir, tmpl.Resource, name)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got
}

func TestContainerTemplateRoundTrip(t *testing.T) {
	zero := 0
	in := &Template{
		Resource:    "container",
		Description: "llm endpoint",
		Container: &ContainerSpec{
			Spot:            true,
			Compute:         "RTX PRO 6000",
			ComputeSize:     2,
			Image:           "ghcr.io/me/llm:v1.2",
			RegistryCreds:   "ghcr-creds",
			Port:            8080,
			HealthcheckPort: 9090,
			HealthcheckPath: "/readyz",
			Env:             map[string]string{"HF_HOME": "/data/.huggingface"},
			EnvSecret:       map[string]string{"HF_TOKEN": "hf-token"},
			Entrypoint:      []string{"python"},
			Cmd:             []string{"serve.py", "--fast"},
			MinReplicas:     &zero,
			MaxReplicas:     5,
			Concurrency:     4,
			QueuePreset:     "cost-saver",
			CPUUtil:         75,
			ScaleUpDelay:    "10s",
			ScaleDownDelay:  "5m",
			RequestTTL:      "2m",
			SecretMounts:    []string{"hf-token:/etc/hf/token"},
		},
	}

	got := saveAndReload(t, in)
	c := got.Container
	if c == nil {
		t.Fatal("container block lost on round trip")
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"spot", c.Spot, true},
		{"compute", c.Compute, "RTX PRO 6000"},
		{"compute_size", c.ComputeSize, 2},
		{"image", c.Image, "ghcr.io/me/llm:v1.2"},
		{"registry_creds", c.RegistryCreds, "ghcr-creds"},
		{"port", c.Port, 8080},
		{"healthcheck_port", c.HealthcheckPort, 9090},
		{"healthcheck_path", c.HealthcheckPath, "/readyz"},
		{"max_replicas", c.MaxReplicas, 5},
		{"concurrency", c.Concurrency, 4},
		{"queue_preset", c.QueuePreset, "cost-saver"},
		{"queue_load", c.QueueLoad, 0},
		{"cpu_util", c.CPUUtil, 75},
		{"gpu_util", c.GPUUtil, 0},
		{"scale_up_delay", c.ScaleUpDelay, "10s"},
		{"scale_down_delay", c.ScaleDownDelay, "5m"},
		{"request_ttl", c.RequestTTL, "2m"},
		{"env", c.Env, map[string]string{"HF_HOME": "/data/.huggingface"}},
		{"env_secret", c.EnvSecret, map[string]string{"HF_TOKEN": "hf-token"}},
		{"entrypoint", c.Entrypoint, []string{"python"}},
		{"cmd", c.Cmd, []string{"serve.py", "--fast"}},
		{"secret_mounts", c.SecretMounts, []string{"hf-token:/etc/hf/token"}},
		{"description", got.Description, "llm endpoint"},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			t.Errorf("%s: got %v, want %v", check.name, check.got, check.want)
		}
	}
}

// min_replicas: 0 must survive — 0 is a real value (scale-to-zero), which is
// why the field is a pointer.
func TestContainerTemplateMinReplicasZero(t *testing.T) {
	zero := 0
	got := saveAndReload(t, &Template{
		Resource:  "container",
		Container: &ContainerSpec{Image: "nginx:1.27", MinReplicas: &zero},
	})
	if got.Container.MinReplicas == nil {
		t.Fatal("min_replicas: 0 dropped by omitempty")
	}
	if *got.Container.MinReplicas != 0 {
		t.Fatalf("min_replicas = %d, want 0", *got.Container.MinReplicas)
	}
}

// The compatibility guarantee, as a literal string (not generated from the
// struct, so a struct change can't silently rewrite the expectation):
// a VM template written before the container split loads unchanged.
const legacyVMTemplate = `resource: vm
billing_type: on-demand
contract: PAY_AS_YOU_GO
kind: gpu
instance_type: 1RTXPRO6000.30V
location: FIN-03
image: Ubuntu 24.04 + CUDA 13.0 Open + Docker
os_volume_size: 50
storage:
    - type: shared
      size: 100
storage_skip: false
ssh_keys:
    - meng@datacrunch.io
startup_script: bootstrap
startup_script_skip: false
hostname_pattern: '{random}-train-{location}'
description: gpu training box
`

func TestLegacyVMTemplateLoadsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vm", "gpu-training.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacyVMTemplate), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("legacy VM template fails to load: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"resource", got.Resource, "vm"},
		{"billing_type", got.BillingType, "on-demand"},
		{"contract", got.Contract, "PAY_AS_YOU_GO"},
		{"kind", got.Kind, "gpu"},
		{"instance_type", got.InstanceType, "1RTXPRO6000.30V"},
		{"location", got.Location, "FIN-03"},
		{"image", got.Image, "Ubuntu 24.04 + CUDA 13.0 Open + Docker"},
		{"os_volume_size", got.OSVolumeSize, 50},
		{"storage", got.Storage, []StorageSpec{{Type: "shared", Size: 100}}},
		{"ssh_keys", got.SSHKeys, []string{"meng@datacrunch.io"}},
		{"startup_script", got.StartupScript, "bootstrap"},
		{"hostname_pattern", got.HostnamePattern, "{random}-train-{location}"},
		{"description", got.Description, "gpu training box"},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.got, check.want) {
			t.Errorf("legacy drift in %s: got %v, want %v", check.name, check.got, check.want)
		}
	}
	if got.Container != nil {
		t.Fatal("VM template gained a container block")
	}

	// Note: "unchanged" is asserted field-by-field on load, the level users
	// depend on; a re-SAVE is NOT byte-identical by design (omitempty drops
	// explicitly-false legacy lines like `storage_skip: false`) — pre-existing
	// behavior, out of this order's scope.
}

func TestValidateRejectsBadShapes(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "container without container block",
			yaml: "resource: container\nimage: nginx:1.27\n",
			// image is also a VM field; the container-block check fires first
			wantErr: `requires the "container" block`,
		},
		{
			name:    "container with VM field set",
			yaml:    "resource: container\ninstance_type: 1RTXPRO6000.30V\ncontainer:\n  image: nginx:1.27\n",
			wantErr: "instance_type",
		},
		{
			name:    "vm with container block",
			yaml:    "resource: vm\ncontainer:\n  image: nginx:1.27\n",
			wantErr: `must not set the "container" block`,
		},
		{
			name:    "unknown resource",
			yaml:    "resource: batchjob\n",
			wantErr: `unknown resource "batchjob"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bad.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFromPath(path)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q lacks %q", err, tc.wantErr)
			}
			// Rule: the failing file path is named in the message.
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("error %q lacks the file path %q", err, path)
			}
		})
	}
}

// resource absent == legacy vm, and it still validates.
func TestValidateResourceAbsentMeansVM(t *testing.T) {
	tmpl := &Template{InstanceType: "1V100.6V"}
	if err := tmpl.Validate(); err != nil {
		t.Fatalf("absent resource must read as vm: %v", err)
	}
}
