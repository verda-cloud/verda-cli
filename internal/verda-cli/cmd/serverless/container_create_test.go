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
	"time"
)

// validOpts returns a containerCreateOptions that passes validate() — used as
// a baseline each test tweaks a single field on.
func validOpts() *containerCreateOptions {
	return &containerCreateOptions{
		Name:               "my-endpoint",
		Image:              "ghcr.io/org/app:v1.2",
		Compute:            "RTX4500Ada",
		ComputeSize:        1,
		Port:               80,
		HealthcheckPath:    defaultHealthcheckPath,
		MinReplicas:        0,
		MaxReplicas:        3,
		Concurrency:        1,
		QueuePreset:        presetBalanced,
		ScaleDownDelay:     300 * time.Second,
		RequestTTL:         300 * time.Second,
		GeneralStorageSize: defaultGeneralStorageGiB,
		SHMSize:            defaultSHMMiB,
	}
}

func TestContainerRequest_HappyPath(t *testing.T) {
	opts := validOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Name != "my-endpoint" {
		t.Errorf("name: got %q", req.Name)
	}
	if req.Compute.Name != "RTX4500Ada" || req.Compute.Size != 1 {
		t.Errorf("compute: got %+v", req.Compute)
	}
	if req.IsSpot {
		t.Errorf("spot should be false by default")
	}
	if len(req.Containers) != 1 {
		t.Fatalf("containers count: got %d", len(req.Containers))
	}
	c := req.Containers[0]
	if c.Image != "ghcr.io/org/app:v1.2" {
		t.Errorf("image: got %q", c.Image)
	}
	if c.ExposedPort != 80 {
		t.Errorf("port: got %d", c.ExposedPort)
	}
	if c.Healthcheck == nil || !c.Healthcheck.Enabled {
		t.Errorf("healthcheck: expected enabled, got %+v", c.Healthcheck)
	}
	if c.Healthcheck.Port != 80 {
		t.Errorf("healthcheck port: got %d, want 80 (defaults to exposed)", c.Healthcheck.Port)
	}
	if c.Healthcheck.Path != "/health" {
		t.Errorf("healthcheck path: got %q", c.Healthcheck.Path)
	}
	if req.Scaling.ScalingTriggers == nil || req.Scaling.ScalingTriggers.QueueLoad == nil {
		t.Fatalf("scaling triggers missing")
	}
	if req.Scaling.ScalingTriggers.QueueLoad.Threshold != queueLoadBalanced {
		t.Errorf("balanced preset should map to %d, got %v", queueLoadBalanced, req.Scaling.ScalingTriggers.QueueLoad.Threshold)
	}
	// General storage + SHM mounts should be present by default.
	if len(c.VolumeMounts) != 2 {
		t.Errorf("expected 2 default mounts (general + shm), got %d: %+v", len(c.VolumeMounts), c.VolumeMounts)
	}
}

func TestContainerRequest_RejectsLatest(t *testing.T) {
	opts := validOpts()
	opts.Image = "ghcr.io/org/app:latest"
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestContainerRequest_PresetMapping(t *testing.T) {
	cases := []struct {
		preset    string
		custom    int
		wantLoad  float64
		expectErr bool
	}{
		{presetInstant, 0, queueLoadInstant, false},
		{presetBalanced, 0, queueLoadBalanced, false},
		{presetCostSaver, 0, queueLoadCostSaver, false},
		{"", 0, queueLoadBalanced, false}, // empty defaults to balanced
		{presetCustom, 42, 42, false},
		{"", 17, 17, false}, // --queue-load alone implies custom
		{presetCustom, 0, 0, true},
		{"unknown", 0, 0, true},
		{presetCustom, 1001, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.preset+":"+strconvItoa(tc.custom), func(t *testing.T) {
			opts := validOpts()
			opts.QueuePreset = tc.preset
			opts.QueueLoad = tc.custom
			req, err := opts.request()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := req.Scaling.ScalingTriggers.QueueLoad.Threshold
			if got != tc.wantLoad {
				t.Errorf("preset %q custom %d → got threshold %v, want %v", tc.preset, tc.custom, got, tc.wantLoad)
			}
		})
	}
}

func TestContainerRequest_HealthcheckOff(t *testing.T) {
	opts := validOpts()
	opts.HealthcheckOff = true
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Containers[0].Healthcheck != nil {
		t.Errorf("expected nil healthcheck, got %+v", req.Containers[0].Healthcheck)
	}
}

func TestContainerRequest_Spot(t *testing.T) {
	opts := validOpts()
	opts.Spot = true
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !req.IsSpot {
		t.Errorf("expected IsSpot=true")
	}
}

func TestContainerRequest_CPUGPUUtilTriggers(t *testing.T) {
	opts := validOpts()
	opts.CPUUtil = 70
	opts.GPUUtil = 80
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	tr := req.Scaling.ScalingTriggers
	if tr.CPUUtilization == nil || !tr.CPUUtilization.Enabled || tr.CPUUtilization.Threshold != 70 {
		t.Errorf("cpu trigger: got %+v", tr.CPUUtilization)
	}
	if tr.GPUUtilization == nil || !tr.GPUUtilization.Enabled || tr.GPUUtilization.Threshold != 80 {
		t.Errorf("gpu trigger: got %+v", tr.GPUUtilization)
	}
}

func TestContainerRequest_EnvMix(t *testing.T) {
	opts := validOpts()
	opts.Env = []string{"HF_HOME=/data/.hf", "DEBUG=1"}
	opts.EnvSecret = []string{"TOKEN=my-secret"}
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	env := req.Containers[0].Env
	if len(env) != 3 {
		t.Fatalf("env count: got %d, want 3 — %+v", len(env), env)
	}
	wantTypes := []string{envTypePlain, envTypePlain, envTypeSecret}
	for i, want := range wantTypes {
		if env[i].Type != want {
			t.Errorf("env[%d] type: got %q, want %q", i, env[i].Type, want)
		}
	}
}

func TestContainerRequest_RegistryCreds(t *testing.T) {
	opts := validOpts()
	opts.RegistryCreds = "my-ghcr"
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	rs := req.ContainerRegistrySettings
	if !rs.IsPrivate {
		t.Errorf("expected IsPrivate=true")
	}
	if rs.Credentials == nil || rs.Credentials.Name != "my-ghcr" {
		t.Errorf("creds: got %+v", rs.Credentials)
	}
}

func TestContainerRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*containerCreateOptions)
		wantErr string
	}{
		{"empty name", func(o *containerCreateOptions) { o.Name = "" }, "required"},
		{"invalid name", func(o *containerCreateOptions) { o.Name = "My_Endpoint" }, "lowercase"},
		{"latest tag", func(o *containerCreateOptions) { o.Image = "nginx:latest" }, "latest"},
		{"zero compute size", func(o *containerCreateOptions) { o.ComputeSize = 0 }, "compute-size"},
		{"negative min", func(o *containerCreateOptions) { o.MinReplicas = -1 }, "min-replicas"},
		{"max < min", func(o *containerCreateOptions) { o.MinReplicas = 5; o.MaxReplicas = 3 }, "max-replicas"},
		{"zero concurrency", func(o *containerCreateOptions) { o.Concurrency = 0 }, "concurrency"},
		{"bad port", func(o *containerCreateOptions) { o.Port = 99999 }, "port"},
		{"bad cpu-util", func(o *containerCreateOptions) { o.CPUUtil = 150 }, "cpu-util"},
		{"bad gpu-util", func(o *containerCreateOptions) { o.GPUUtil = -1 }, "gpu-util"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := validOpts()
			tc.mutate(opts)
			_, err := opts.request()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// strconvItoa is a tiny helper so the test table above can embed ints in
// subtest names without an import dance.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
