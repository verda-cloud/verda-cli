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

// Wire-format regression tests for create payloads; change assertions whenever
// request assembly changes (see cmd/serverless/CLAUDE.md).

package serverless

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

// recordingServer stores POST bodies from create endpoints for JSON assertions.
type recordingServer struct {
	mu          sync.Mutex
	containerOK []byte
	jobOK       []byte
	srv         *httptest.Server
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	rec := &recordingServer{}
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
		rec.containerOK = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name:            "cli-test",
			EndpointBaseURL: "https://containers.verda.test/cli-test",
		})
	})

	mux.HandleFunc("POST /job-deployments", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.jobOK = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.JobDeployment{Name: "cli-test-job"})
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *recordingServer) containerBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.containerOK
}

func (r *recordingServer) jobBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobOK
}

func newTestFactory(t *testing.T, baseURL string) *cmdutil.TestFactory {
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
		AgentModeOverride:    true, // skip wizard + confirm prompt
	}
}

// TestContainerCreate_WireFormat runs `verda container create --agent ...`
// against an in-process server and asserts the JSON the CLI actually sends.
// This is the test that would have caught the production bug where the CLI
// sent type:"shared" with size_in_mb and the API rejected it with
// `volume_mounts.0.volume_id should not be null or undefined`.
func TestContainerCreate_WireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"create",
		"--name", "cli-test",
		"--image", "ghcr.io/org/app:v1.2",
		"--compute", "RTX4500Ada", "--compute-size", "1",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.containerBody()
	if body == nil {
		t.Fatalf("server did not receive POST /container-deployments\nstderr:\n%s", stderr.String())
	}

	var got struct {
		Name       string `json:"name"`
		IsSpot     bool   `json:"is_spot"`
		Containers []struct {
			Image        string `json:"image"`
			ExposedPort  int    `json:"exposed_port"`
			VolumeMounts []struct {
				Type      string `json:"type"`
				MountPath string `json:"mount_path"`
				SizeInMB  int    `json:"size_in_mb"`
				VolumeID  string `json:"volume_id"`
			} `json:"volume_mounts"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal request body: %v\nbody: %s", err, body)
	}

	if got.Name != "cli-test" {
		t.Errorf("name: got %q, want cli-test", got.Name)
	}
	if len(got.Containers) != 1 {
		t.Fatalf("containers: got %d, want 1", len(got.Containers))
	}
	c := got.Containers[0]
	if c.Image != "ghcr.io/org/app:v1.2" {
		t.Errorf("image: got %q", c.Image)
	}
	if len(c.VolumeMounts) != 1 {
		t.Fatalf("volume_mounts: got %d entries, want exactly 1 (scratch /data)\nbody: %s", len(c.VolumeMounts), body)
	}
	m := c.VolumeMounts[0]
	if m.Type != "scratch" {
		t.Errorf("volume_mounts[0].type: got %q, want %q — sending %q tells the API this is a named persistent volume and it will reject the request looking for volume_id", m.Type, "scratch", m.Type)
	}
	if m.MountPath != "/data" {
		t.Errorf("volume_mounts[0].mount_path: got %q, want /data", m.MountPath)
	}
	if m.SizeInMB != 0 {
		t.Errorf("volume_mounts[0].size_in_mb: got %d, want 0 — scratch is server-allocated; sending a size makes the API treat the mount as named/shared", m.SizeInMB)
	}
	if m.VolumeID != "" {
		t.Errorf("volume_mounts[0].volume_id: got %q, want empty", m.VolumeID)
	}

	// Stdout in agent mode is JSON; verify it parsed the synthetic response.
	if !strings.Contains(stdout.String(), "cli-test") {
		t.Errorf("stdout should contain deployment name; got:\n%s", stdout.String())
	}
}

// TestBatchjobCreate_WireFormat is the batchjob counterpart. Same volume_mounts
// contract; additionally asserts deadline_seconds and that IsSpot is NOT sent
// (the API has no IsSpot field on job deployments).
func TestBatchjobCreate_WireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdBatchjob(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"create",
		"--name", "cli-test-job",
		"--image", "ghcr.io/org/embedder:v1",
		"--compute", "RTX4500Ada", "--compute-size", "1",
		"--deadline", "30m",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("batchjob create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.jobBody()
	if body == nil {
		t.Fatalf("server did not receive POST /job-deployments\nstderr:\n%s", stderr.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal request body: %v\nbody: %s", err, body)
	}
	if _, present := raw["is_spot"]; present {
		t.Errorf("job request must not include is_spot (API has no IsSpot field for jobs); body: %s", body)
	}

	var got struct {
		Name    string `json:"name"`
		Scaling *struct {
			DeadlineSeconds int `json:"deadline_seconds"`
			MaxReplicaCount int `json:"max_replica_count"`
		} `json:"scaling"`
		Containers []struct {
			Image        string `json:"image"`
			VolumeMounts []struct {
				Type      string `json:"type"`
				MountPath string `json:"mount_path"`
				SizeInMB  int    `json:"size_in_mb"`
				VolumeID  string `json:"volume_id"`
			} `json:"volume_mounts"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal typed: %v\nbody: %s", err, body)
	}

	if got.Name != "cli-test-job" {
		t.Errorf("name: got %q, want cli-test-job", got.Name)
	}
	if got.Scaling == nil || got.Scaling.DeadlineSeconds != 30*60 {
		t.Errorf("scaling.deadline_seconds: got %+v, want 1800", got.Scaling)
	}
	if len(got.Containers) != 1 || len(got.Containers[0].VolumeMounts) != 1 {
		t.Fatalf("expected 1 container with 1 volume_mount; body: %s", body)
	}
	m := got.Containers[0].VolumeMounts[0]
	if m.Type != "scratch" || m.MountPath != "/data" {
		t.Errorf("volume_mounts[0]: got {type:%q path:%q}, want {scratch /data}", m.Type, m.MountPath)
	}
	if m.SizeInMB != 0 || m.VolumeID != "" {
		t.Errorf("scratch mount must not send size_in_mb or volume_id; got size=%d volume_id=%q", m.SizeInMB, m.VolumeID)
	}
}

// TestContainerCreate_SecretMountWireFormat covers the second branch of
// buildVolumeMounts: with one --secret-mount flag, the request should contain
// two mounts — the auto scratch /data and the secret mount.
func TestContainerCreate_SecretMountWireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"create",
		"--name", "cli-test",
		"--image", "ghcr.io/org/app:v1.2",
		"--compute", "RTX4500Ada", "--compute-size", "1",
		"--secret-mount", "my-secret:/etc/creds/token",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container create failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.containerBody()
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
	if mounts[1].Type != "secret" || mounts[1].SecretName != "my-secret" || mounts[1].MountPath != "/etc/creds/token" {
		t.Errorf("second mount must be the secret, got %+v", mounts[1])
	}
}
