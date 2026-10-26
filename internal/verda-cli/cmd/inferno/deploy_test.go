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
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func validInfernoOpts() *infernoDeployOptions {
	return &infernoDeployOptions{
		Name:           "my-model",
		Image:          "vccr.io/proj/model-worker:v1.0",
		Compute:        "RTX PRO 6000",
		ComputeSize:    1,
		MinReplicas:    0,
		MaxReplicas:    defaultInfernoMaxReplicas,
		Concurrency:    defaultInfernoConcurrency,
		QueuePreset:    presetInstant,
		ScaleDownDelay: defaultInfernoScaleDownDelay,
		RequestTTL:     defaultInfernoRequestTTL,
	}
}

func TestInfernoRequest_HappyPath_TopLevel(t *testing.T) {
	opts := validInfernoOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Name != "my-model" {
		t.Errorf("name: got %q", req.Name)
	}
	if req.Compute.Name != "RTX PRO 6000" || req.Compute.Size != 1 {
		t.Errorf("compute: got %+v", req.Compute)
	}
	if req.IsSpot {
		t.Errorf("spot should be false by default")
	}
	if len(req.Containers) != 1 {
		t.Fatalf("containers count: got %d", len(req.Containers))
	}
}

func TestInfernoRequest_HappyPath_Container(t *testing.T) {
	opts := validInfernoOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	c := req.Containers[0]
	if c.Image != "vccr.io/proj/model-worker:v1.0" {
		t.Errorf("image: got %q", c.Image)
	}
	if c.ExposedPort != infernoExposedPort {
		t.Errorf("port: got %d, want %d", c.ExposedPort, infernoExposedPort)
	}
	if c.EntrypointOverrides == nil || c.EntrypointOverrides.Enabled {
		t.Errorf("entrypoint overrides should be disabled, got %+v", c.EntrypointOverrides)
	}
	if c.Healthcheck == nil || !c.Healthcheck.Enabled {
		t.Fatalf("healthcheck: expected enabled, got %+v", c.Healthcheck)
	}
	if c.Healthcheck.Port != infernoExposedPort {
		t.Errorf("healthcheck port: got %d, want %d", c.Healthcheck.Port, infernoExposedPort)
	}
	if c.Healthcheck.Path != infernoHealthcheckPath {
		t.Errorf("healthcheck path: got %q, want %q", c.Healthcheck.Path, infernoHealthcheckPath)
	}
}

func TestInfernoRequest_HappyPath_ScalingAndMounts(t *testing.T) {
	opts := validInfernoOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.Scaling.ScalingTriggers == nil || req.Scaling.ScalingTriggers.QueueLoad == nil {
		t.Fatalf("scaling triggers missing")
	}
	if req.Scaling.ScalingTriggers.QueueLoad.Threshold != float64(queueLoadInstant) {
		t.Errorf("instant preset should map to %d, got %v", queueLoadInstant, req.Scaling.ScalingTriggers.QueueLoad.Threshold)
	}
	c := req.Containers[0]
	if len(c.VolumeMounts) != 1 {
		t.Fatalf("expected 1 default mount (scratch /data), got %d: %+v", len(c.VolumeMounts), c.VolumeMounts)
	}
	if c.VolumeMounts[0].Type != mountTypeScratch || c.VolumeMounts[0].MountPath != defaultGeneralStoragePath {
		t.Errorf("default mount: got %+v, want scratch at /data", c.VolumeMounts[0])
	}
}

func TestInfernoRequest_RejectsLatest(t *testing.T) {
	opts := validInfernoOpts()
	opts.Image = "vccr.io/proj/model-worker:latest"
	_, err := opts.request()
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestInfernoRequest_Spot(t *testing.T) {
	opts := validInfernoOpts()
	opts.Spot = true
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !req.IsSpot {
		t.Errorf("expected IsSpot=true")
	}
}

func TestInfernoRequest_RegistryCreds(t *testing.T) {
	opts := validInfernoOpts()
	opts.RegistryCreds = "my-creds"
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	rs := req.ContainerRegistrySettings
	if !rs.IsPrivate {
		t.Errorf("expected IsPrivate=true")
	}
	if rs.Credentials == nil || rs.Credentials.Name != "my-creds" {
		t.Errorf("creds: got %+v", rs.Credentials)
	}
}

func TestInfernoRequest_RegistryPublicDefault(t *testing.T) {
	opts := validInfernoOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if req.ContainerRegistrySettings.IsPrivate {
		t.Errorf("expected IsPrivate=false for public default")
	}
	if req.ContainerRegistrySettings.Credentials != nil {
		t.Errorf("expected nil credentials for public, got %+v", req.ContainerRegistrySettings.Credentials)
	}
}

func TestInfernoRequest_EnvMix(t *testing.T) {
	opts := validInfernoOpts()
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

func TestInfernoRequest_SecretMounts(t *testing.T) {
	opts := validInfernoOpts()
	opts.SecretMounts = []string{"api-key:/etc/creds/token"}
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	mounts := req.Containers[0].VolumeMounts
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts (scratch + secret), got %d", len(mounts))
	}
	if mounts[0].Type != mountTypeScratch || mounts[0].MountPath != defaultGeneralStoragePath {
		t.Errorf("first mount must be scratch /data, got %+v", mounts[0])
	}
	if mounts[1].Type != mountTypeSecret || mounts[1].SecretName != "api-key" || mounts[1].MountPath != "/etc/creds/token" {
		t.Errorf("second mount must be the secret, got %+v", mounts[1])
	}
}

func TestInfernoRequest_PresetMapping(t *testing.T) {
	cases := []struct {
		preset    string
		custom    int
		wantLoad  float64
		expectErr bool
	}{
		{presetInstant, 0, float64(queueLoadInstant), false},
		{presetBalanced, 0, float64(queueLoadBalanced), false},
		{presetCostSaver, 0, float64(queueLoadCostSaver), false},
		{"", 0, float64(queueLoadBalanced), false},
		{presetCustom, 42, 42, false},
		{"", 17, 17, false},
		{presetCustom, 0, 0, true},
		{"unknown", 0, 0, true},
		{presetCustom, 1001, 0, true},
	}
	for _, tc := range cases {
		name := tc.preset
		if name == "" {
			name = "(empty)"
		}
		t.Run(name+":"+strings.Repeat("c", min(tc.custom, 1)), func(t *testing.T) {
			opts := validInfernoOpts()
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

func TestInfernoRequest_ScalingFields(t *testing.T) {
	opts := validInfernoOpts()
	opts.MinReplicas = 1
	opts.MaxReplicas = 5
	opts.Concurrency = 4
	opts.ScaleUpDelay = 30 * time.Second
	opts.ScaleDownDelay = 600 * time.Second
	opts.RequestTTL = 120 * time.Second

	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	s := req.Scaling
	if s.MinReplicaCount != 1 {
		t.Errorf("min replicas: got %d", s.MinReplicaCount)
	}
	if s.MaxReplicaCount != 5 {
		t.Errorf("max replicas: got %d", s.MaxReplicaCount)
	}
	if s.ConcurrentRequestsPerReplica != 4 {
		t.Errorf("concurrency: got %d", s.ConcurrentRequestsPerReplica)
	}
	if s.ScaleUpPolicy == nil || s.ScaleUpPolicy.DelaySeconds != 30 {
		t.Errorf("scale up delay: got %+v", s.ScaleUpPolicy)
	}
	if s.ScaleDownPolicy == nil || s.ScaleDownPolicy.DelaySeconds != 600 {
		t.Errorf("scale down delay: got %+v", s.ScaleDownPolicy)
	}
	if s.QueueMessageTTLSeconds != 120 {
		t.Errorf("request TTL: got %d", s.QueueMessageTTLSeconds)
	}
}

func TestInfernoRequest_ValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*infernoDeployOptions)
		wantErr string
	}{
		{"empty name", func(o *infernoDeployOptions) { o.Name = "" }, "required"},
		{"invalid name", func(o *infernoDeployOptions) { o.Name = "My_Model" }, "lowercase"},
		{"latest tag", func(o *infernoDeployOptions) { o.Image = "nginx:latest" }, "latest"},
		{"implicit latest", func(o *infernoDeployOptions) { o.Image = "nginx" }, "latest"},
		{"zero compute size", func(o *infernoDeployOptions) { o.ComputeSize = 0 }, "compute-size"},
		{"negative min", func(o *infernoDeployOptions) { o.MinReplicas = -1 }, "min-replicas"},
		{"max < min", func(o *infernoDeployOptions) { o.MinReplicas = 5; o.MaxReplicas = 3 }, "max-replicas"},
		{"zero max", func(o *infernoDeployOptions) { o.MaxReplicas = 0 }, "max-replicas"},
		{"zero concurrency", func(o *infernoDeployOptions) { o.Concurrency = 0 }, "concurrency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := validInfernoOpts()
			tc.mutate(opts)
			_, err := opts.request()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestInfernoRequest_InfernoDefaults(t *testing.T) {
	opts := validInfernoOpts()
	req, err := opts.request()
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	c := req.Containers[0]
	if c.ExposedPort != 5001 {
		t.Errorf("Inferno port: got %d, want 5001", c.ExposedPort)
	}
	if c.Healthcheck.Path != "/health" {
		t.Errorf("Inferno healthcheck path: got %q, want /health", c.Healthcheck.Path)
	}
	if c.Healthcheck.Port != 5001 {
		t.Errorf("Inferno healthcheck port: got %d, want 5001", c.Healthcheck.Port)
	}
	if c.EntrypointOverrides == nil || c.EntrypointOverrides.Enabled {
		t.Errorf("Inferno entrypoint overrides must be disabled")
	}
}

func TestInfernoDeploy_AgentMode_MissingFlags(t *testing.T) {
	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true

	ioStreams := cmdutil.IOStreams{In: nil, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runInfernoDeploy(cmd, f, ioStreams, &infernoDeployOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("expected MISSING_REQUIRED_FLAGS, got code=%q msg=%q", agentErr.Code, agentErr.Message)
	}
	if errors.Is(err, cmdutil.ErrNoClient) {
		t.Fatalf("auth error leaked through — agent-mode flag check must run before VerdaClient: %v", err)
	}
}

func TestInfernoDeploy_AgentMode_PartialFlags(t *testing.T) {
	cases := []struct {
		name    string
		opts    infernoDeployOptions
		missing []string
	}{
		{
			"missing all",
			infernoDeployOptions{},
			[]string{"--name", "--image", "--compute"},
		},
		{
			"only name",
			infernoDeployOptions{Name: "my-model"},
			[]string{"--image", "--compute"},
		},
		{
			"name and image",
			infernoDeployOptions{Name: "my-model", Image: "img:v1"},
			[]string{"--compute"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := cmdutil.NewTestFactory(nil)
			f.AgentModeOverride = true

			ioStreams := cmdutil.IOStreams{In: nil, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())

			err := runInfernoDeploy(cmd, f, ioStreams, &tc.opts)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var agentErr *cmdutil.AgentError
			if !errors.As(err, &agentErr) {
				t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
			}
			detail, ok := agentErr.Details["missing"]
			if !ok {
				t.Fatalf("expected Details[\"missing\"], got %+v", agentErr.Details)
			}
			missingSlice, ok := detail.([]string)
			if !ok {
				t.Fatalf("Details[\"missing\"] is %T, want []string", detail)
			}
			if len(missingSlice) != len(tc.missing) {
				t.Fatalf("missing flags: got %v, want %v", missingSlice, tc.missing)
			}
			for i, want := range tc.missing {
				if missingSlice[i] != want {
					t.Errorf("missing[%d]: got %q, want %q", i, missingSlice[i], want)
				}
			}
		})
	}
}

func TestMissingInfernoDeployFlags(t *testing.T) {
	cases := []struct {
		name     string
		opts     infernoDeployOptions
		expected []string
	}{
		{"all missing", infernoDeployOptions{}, []string{"--name", "--image", "--compute"}},
		{"name set", infernoDeployOptions{Name: "x"}, []string{"--image", "--compute"}},
		{"all set", infernoDeployOptions{Name: "x", Image: "img:v1", Compute: "GPU"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := missingInfernoDeployFlags(&tc.opts)
			if len(got) != len(tc.expected) {
				t.Fatalf("got %v, want %v", got, tc.expected)
			}
			for i, want := range tc.expected {
				if got[i] != want {
					t.Errorf("missing[%d]: got %q, want %q", i, got[i], want)
				}
			}
		})
	}
}
