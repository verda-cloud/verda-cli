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

// Wire-format regression tests for inferno deploy payloads. Change assertions
// whenever request assembly changes — these are the only test layer that
// catches bugs the SDK's ValidateCreateDeploymentRequest does not.

package inferno

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type infernoRecordingServer struct {
	mu   sync.Mutex
	body []byte
	srv  *httptest.Server
}

func newInfernoRecordingServer(t *testing.T) *infernoRecordingServer {
	t.Helper()
	rec := &infernoRecordingServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("POST /container-deployments", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.body = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name:            "inferno-test",
			EndpointBaseURL: "https://containers.verda.test/inferno-test",
		})
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *infernoRecordingServer) capturedBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.body
}

func newInfernoTestFactory(t *testing.T, baseURL string) *cmdutil.TestFactory {
	t.Helper()
	client, err := verda.NewClient(
		verda.WithBaseURL(baseURL),
		verda.WithClientID("test"),
		verda.WithClientSecret("test"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	return &cmdutil.TestFactory{
		ClientOverride:       client,
		OutputFormatOverride: "json",
		AgentModeOverride:    true,
	}
}

type wireFormatPayload struct {
	Name    string `json:"name"`
	IsSpot  bool   `json:"is_spot"`
	Compute struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	} `json:"compute"`
	Scaling struct {
		MinReplicaCount              int `json:"min_replica_count"`
		MaxReplicaCount              int `json:"max_replica_count"`
		ConcurrentRequestsPerReplica int `json:"concurrent_requests_per_replica"`
		QueueMessageTTLSeconds       int `json:"queue_message_ttl_seconds"`
	} `json:"scaling"`
	Containers []struct {
		Image       string `json:"image"`
		ExposedPort int    `json:"exposed_port"`
		Healthcheck *struct {
			Enabled bool   `json:"enabled"`
			Port    int    `json:"port"`
			Path    string `json:"path"`
		} `json:"healthcheck"`
		EntrypointOverrides *struct {
			Enabled bool `json:"enabled"`
		} `json:"entrypoint_overrides"`
		VolumeMounts []struct {
			Type      string `json:"type"`
			MountPath string `json:"mount_path"`
			SizeInMB  int    `json:"size_in_mb"`
			VolumeID  string `json:"volume_id"`
		} `json:"volume_mounts"`
		Env []struct {
			Type  string `json:"type"`
			Name  string `json:"name"`
			Value string `json:"value_or_reference_to_secret"`
		} `json:"env"`
	} `json:"containers"`
}

func executeInfernoDeploy(t *testing.T, rec *infernoRecordingServer, f *cmdutil.TestFactory, args []string) (wireFormatPayload, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}
	body := rec.capturedBody()
	if body == nil {
		t.Fatalf("server did not receive POST /container-deployments\nstderr:\n%s", stderr.String())
	}
	var got wireFormatPayload
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal request body: %v\nbody: %s", err, body)
	}
	return got, stdout.String()
}

func TestInfernoDeploy_WireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	got, stdout := executeInfernoDeploy(t, rec, f, []string{
		"deploy",
		"--name", "inferno-test",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000", "--compute-size", "1",
		"--yes",
	})

	if got.Name != "inferno-test" {
		t.Errorf("name: got %q, want inferno-test", got.Name)
	}
	if got.IsSpot {
		t.Errorf("is_spot: got true, want false")
	}
	if got.Compute.Name != "RTX PRO 6000" || got.Compute.Size != 1 {
		t.Errorf("compute: got %+v", got.Compute)
	}
	if len(got.Containers) != 1 {
		t.Fatalf("containers: got %d, want 1", len(got.Containers))
	}
	c := got.Containers[0]
	if c.Image != "vccr.io/proj/model-worker:v1.0" {
		t.Errorf("image: got %q", c.Image)
	}
	if c.ExposedPort != 5001 {
		t.Errorf("exposed_port: got %d, want 5001 (Inferno default)", c.ExposedPort)
	}
	if !strings.Contains(stdout, "inferno-test") {
		t.Errorf("stdout should contain deployment name; got:\n%s", stdout)
	}
}

func TestInfernoDeploy_WireFormat_Healthcheck(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	got, _ := executeInfernoDeploy(t, rec, f, []string{
		"deploy",
		"--name", "hc-test",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--yes",
	})

	c := got.Containers[0]
	if c.Healthcheck == nil {
		t.Fatal("healthcheck: expected non-nil")
	}
	if !c.Healthcheck.Enabled || c.Healthcheck.Port != 5001 || c.Healthcheck.Path != "/health" {
		t.Errorf("healthcheck: got %+v, want enabled port=5001 path=/health", c.Healthcheck)
	}
	if c.EntrypointOverrides == nil {
		t.Fatal("entrypoint_overrides: expected non-nil")
	}
	if c.EntrypointOverrides.Enabled {
		t.Errorf("entrypoint_overrides: got enabled=true, want false")
	}
}

func TestInfernoDeploy_WireFormat_VolumeMounts(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	got, _ := executeInfernoDeploy(t, rec, f, []string{
		"deploy",
		"--name", "mount-test",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--yes",
	})

	c := got.Containers[0]
	if len(c.VolumeMounts) != 1 {
		t.Fatalf("volume_mounts: got %d entries, want exactly 1 (scratch /data)", len(c.VolumeMounts))
	}
	m := c.VolumeMounts[0]
	if m.Type != "scratch" || m.MountPath != "/data" {
		t.Errorf("volume_mounts[0]: got type=%q path=%q, want scratch /data", m.Type, m.MountPath)
	}
	if m.SizeInMB != 0 {
		t.Errorf("volume_mounts[0].size_in_mb: got %d, want 0 (scratch is server-allocated)", m.SizeInMB)
	}
	if m.VolumeID != "" {
		t.Errorf("volume_mounts[0].volume_id: got %q, want empty", m.VolumeID)
	}
}

func TestInfernoDeploy_SpotWireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"deploy",
		"--name", "spot-model",
		"--image", "vccr.io/proj/model-worker:v2.0",
		"--compute", "RTX PRO 6000", "--compute-size", "2",
		"--spot",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.capturedBody()
	var got struct {
		IsSpot  bool `json:"is_spot"`
		Compute struct {
			Size int `json:"size"`
		} `json:"compute"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if !got.IsSpot {
		t.Errorf("is_spot: got false, want true")
	}
	if got.Compute.Size != 2 {
		t.Errorf("compute.size: got %d, want 2", got.Compute.Size)
	}
}

func TestInfernoDeploy_EnvWireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"deploy",
		"--name", "env-model",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--env", "HF_HOME=/data/.huggingface",
		"--env", "DEBUG=1",
		"--env-secret", "HF_TOKEN=hf-token-secret",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.capturedBody()
	var got struct {
		Containers []struct {
			Env []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Value string `json:"value_or_reference_to_secret"`
			} `json:"env"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	env := got.Containers[0].Env
	if len(env) != 3 {
		t.Fatalf("expected 3 env vars, got %d: body=%s", len(env), body)
	}
	if env[0].Type != "plain" || env[0].Name != "HF_HOME" || env[0].Value != "/data/.huggingface" {
		t.Errorf("env[0]: got %+v", env[0])
	}
	if env[1].Type != "plain" || env[1].Name != "DEBUG" || env[1].Value != "1" {
		t.Errorf("env[1]: got %+v", env[1])
	}
	if env[2].Type != "secret" || env[2].Name != "HF_TOKEN" || env[2].Value != "hf-token-secret" {
		t.Errorf("env[2]: got %+v", env[2])
	}
}

func TestInfernoDeploy_SecretMountWireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"deploy",
		"--name", "secret-model",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--secret-mount", "api-key:/etc/creds/token",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.capturedBody()
	var got struct {
		Containers []struct {
			VolumeMounts []struct {
				Type       string `json:"type"`
				MountPath  string `json:"mount_path"`
				SecretName string `json:"secret_name"`
			} `json:"volume_mounts"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	mounts := got.Containers[0].VolumeMounts
	if len(mounts) != 2 {
		t.Fatalf("expected 2 mounts (scratch + secret), got %d: body=%s", len(mounts), body)
	}
	if mounts[0].Type != "scratch" || mounts[0].MountPath != "/data" {
		t.Errorf("first mount must be scratch /data, got %+v", mounts[0])
	}
	if mounts[1].Type != "secret" || mounts[1].SecretName != "api-key" || mounts[1].MountPath != "/etc/creds/token" {
		t.Errorf("second mount must be the secret, got %+v", mounts[1])
	}
}

func TestInfernoDeploy_RegistryCredsWireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"deploy",
		"--name", "private-model",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--registry-creds", "my-ghcr",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.capturedBody()
	var got struct {
		Registry struct {
			IsPrivate   bool `json:"is_private"`
			Credentials *struct {
				Name string `json:"name"`
			} `json:"credentials"`
		} `json:"container_registry_settings"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if !got.Registry.IsPrivate {
		t.Errorf("is_private: got false, want true")
	}
	if got.Registry.Credentials == nil || got.Registry.Credentials.Name != "my-ghcr" {
		t.Errorf("credentials: got %+v", got.Registry.Credentials)
	}
}

func TestInfernoDeploy_ScalingWireFormat(t *testing.T) {
	t.Parallel()
	rec := newInfernoRecordingServer(t)
	f := newInfernoTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdInferno(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"deploy",
		"--name", "scaled-model",
		"--image", "vccr.io/proj/model-worker:v1.0",
		"--compute", "RTX PRO 6000",
		"--min-replicas", "1",
		"--max-replicas", "4",
		"--concurrency", "3",
		"--queue-preset", "balanced",
		"--scale-up-delay", "10s",
		"--scale-down-delay", "120s",
		"--request-ttl", "60s",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inferno deploy failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.capturedBody()
	var got struct {
		Scaling struct {
			MinReplicaCount              int `json:"min_replica_count"`
			MaxReplicaCount              int `json:"max_replica_count"`
			ConcurrentRequestsPerReplica int `json:"concurrent_requests_per_replica"`
			QueueMessageTTLSeconds       int `json:"queue_message_ttl_seconds"`
			ScaleUpPolicy                *struct {
				DelaySeconds int `json:"delay_seconds"`
			} `json:"scale_up_policy"`
			ScaleDownPolicy *struct {
				DelaySeconds int `json:"delay_seconds"`
			} `json:"scale_down_policy"`
			ScalingTriggers *struct {
				QueueLoad *struct {
					Threshold float64 `json:"threshold"`
				} `json:"queue_load"`
			} `json:"scaling_triggers"`
		} `json:"scaling"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}

	s := got.Scaling
	if s.MinReplicaCount != 1 {
		t.Errorf("min_replica_count: got %d, want 1", s.MinReplicaCount)
	}
	if s.MaxReplicaCount != 4 {
		t.Errorf("max_replica_count: got %d, want 4", s.MaxReplicaCount)
	}
	if s.ConcurrentRequestsPerReplica != 3 {
		t.Errorf("concurrent_requests_per_replica: got %d, want 3", s.ConcurrentRequestsPerReplica)
	}
	if s.QueueMessageTTLSeconds != 60 {
		t.Errorf("queue_message_ttl_seconds: got %d, want 60", s.QueueMessageTTLSeconds)
	}
	if s.ScaleUpPolicy == nil || s.ScaleUpPolicy.DelaySeconds != 10 {
		t.Errorf("scale_up_policy: got %+v, want delay=10", s.ScaleUpPolicy)
	}
	if s.ScaleDownPolicy == nil || s.ScaleDownPolicy.DelaySeconds != 120 {
		t.Errorf("scale_down_policy: got %+v, want delay=120", s.ScaleDownPolicy)
	}
	if s.ScalingTriggers == nil || s.ScalingTriggers.QueueLoad == nil {
		t.Fatal("scaling_triggers.queue_load: expected non-nil")
	}
	if s.ScalingTriggers.QueueLoad.Threshold != float64(queueLoadBalanced) {
		t.Errorf("queue_load.threshold: got %v, want %d", s.ScalingTriggers.QueueLoad.Threshold, queueLoadBalanced)
	}
}
