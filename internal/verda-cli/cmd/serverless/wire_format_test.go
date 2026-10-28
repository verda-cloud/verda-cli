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

// recordingServer stores POST/PATCH bodies from create/update endpoints.
type recordingServer struct {
	mu                      sync.Mutex
	containerOK             []byte
	containerPatchOK        []byte
	containerScalingPatchOK []byte
	jobOK                   []byte
	jobPatchOK              []byte
	srv                     *httptest.Server
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

	// -- Update (GET + PATCH) endpoints --

	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name: r.PathValue("name"),
			Containers: []verda.DeploymentContainer{{
				Name:        "main",
				Image:       verda.ContainerImage{Image: "ghcr.io/org/app:v1.0"},
				ExposedPort: 8080,
				Healthcheck: &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"},
				Env: []verda.ContainerEnvVar{
					{Name: "FOO", ValueOrReferenceToSecret: "bar", Type: "plain"},
					{Name: "SECRET_KEY", ValueOrReferenceToSecret: "my-secret", Type: "secret"},
				},
			}},
			Compute: &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
		})
	})

	mux.HandleFunc("GET /container-deployments/{name}/scaling", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerScalingOptions{
			MinReplicaCount:              1,
			MaxReplicaCount:              3,
			ConcurrentRequestsPerReplica: 1,
			QueueMessageTTLSeconds:       300,
			ScalingTriggers: &verda.ScalingTriggers{
				QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
			},
		})
	})

	mux.HandleFunc("PATCH /container-deployments/{name}/scaling", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.containerScalingPatchOK = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerScalingOptions{
			MinReplicaCount: 0, MaxReplicaCount: 10,
		})
	})

	mux.HandleFunc("PATCH /container-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.containerPatchOK = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{Name: r.PathValue("name")})
	})

	mux.HandleFunc("GET /job-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.JobDeployment{
			Name: r.PathValue("name"),
			Containers: []verda.DeploymentContainer{{
				Name:        "worker",
				Image:       verda.ContainerImage{Image: "ghcr.io/org/embedder:v1"},
				ExposedPort: 8080,
				Env: []verda.ContainerEnvVar{
					{Name: "FOO", ValueOrReferenceToSecret: "bar", Type: "plain"},
				},
			}},
			Compute: &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
			Scaling: &verda.JobScalingOptions{
				MaxReplicaCount:        3,
				DeadlineSeconds:        1800,
				QueueMessageTTLSeconds: 300,
			},
		})
	})

	mux.HandleFunc("PATCH /job-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec.mu.Lock()
		rec.jobPatchOK = body
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.JobDeployment{Name: r.PathValue("name")})
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

func (r *recordingServer) containerPatchBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.containerPatchOK
}

func (r *recordingServer) containerScalingPatchBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.containerScalingPatchOK
}

func (r *recordingServer) jobPatchBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobPatchOK
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

// TestContainerUpdate_WireFormat asserts that updating image + spot sends the
// correct PATCH body: is_spot set, containers merged from current state, and
// no compute/scaling fields (they weren't changed).
func TestContainerUpdate_WireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"update", "cli-test",
		"--image", "ghcr.io/org/app:v2.0",
		"--spot",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container update failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.containerPatchBody()
	if body == nil {
		t.Fatalf("server did not receive PATCH /container-deployments/cli-test\nstderr:\n%s", stderr.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}

	// is_spot must be present and true.
	isSpot, ok := raw["is_spot"]
	if !ok {
		t.Errorf("is_spot missing from PATCH body; body: %s", body)
	} else if isSpot != true {
		t.Errorf("is_spot: got %v, want true", isSpot)
	}

	// compute and scaling must NOT be present (unchanged).
	if _, present := raw["compute"]; present {
		t.Errorf("compute should be omitted (not changed); body: %s", body)
	}
	if _, present := raw["scaling"]; present {
		t.Errorf("scaling should be omitted (not changed); body: %s", body)
	}

	var got struct {
		Containers []struct {
			Name        string `json:"name"`
			Image       string `json:"image"`
			ExposedPort int    `json:"exposed_port"`
			Healthcheck *struct {
				Enabled bool   `json:"enabled"`
				Port    int    `json:"port"`
				Path    string `json:"path"`
			} `json:"healthcheck"`
			Env []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"env"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal typed: %v\nbody: %s", err, body)
	}

	if len(got.Containers) != 1 {
		t.Fatalf("containers: got %d, want 1", len(got.Containers))
	}
	c := got.Containers[0]
	if c.Name != "main" {
		t.Errorf("name: got %q, want main (carried from current)", c.Name)
	}
	if c.Image != "ghcr.io/org/app:v2.0" {
		t.Errorf("image: got %q, want ghcr.io/org/app:v2.0", c.Image)
	}
	if c.ExposedPort != 8080 {
		t.Errorf("exposed_port: got %d, want 8080 (carried)", c.ExposedPort)
	}
	if c.Healthcheck == nil || !c.Healthcheck.Enabled || c.Healthcheck.Port != 8080 || c.Healthcheck.Path != "/health" {
		t.Errorf("healthcheck should be carried from current; got %+v", c.Healthcheck)
	}
	if len(c.Env) != 2 {
		t.Errorf("env: got %d entries, want 2 (carried from current); body: %s", len(c.Env), body)
	}

	if !strings.Contains(stdout.String(), "cli-test") {
		t.Errorf("stdout should contain deployment name; got:\n%s", stdout.String())
	}
}

// TestContainerUpdate_EnvMergeWireFormat verifies that setting --env replaces
// only plain env vars; secret vars from the current deployment are carried over.
func TestContainerUpdate_EnvMergeWireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"update", "cli-test",
		"--env", "NEW=val",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container update failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.containerPatchBody()
	if body == nil {
		t.Fatalf("no PATCH body received")
	}

	var got struct {
		Containers []struct {
			Env []struct {
				Name                     string `json:"name"`
				ValueOrReferenceToSecret string `json:"value_or_reference_to_secret"`
				Type                     string `json:"type"`
			} `json:"env"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if len(got.Containers) != 1 {
		t.Fatalf("containers: got %d, want 1", len(got.Containers))
	}

	envs := got.Containers[0].Env
	// --env replaces all plain vars; secret vars are carried from current.
	// Current had: FOO=bar (plain), SECRET_KEY=my-secret (secret).
	// After: NEW=val (plain from flag), SECRET_KEY=my-secret (carried secret).
	if len(envs) != 2 {
		t.Fatalf("env: got %d entries, want 2 (1 new plain + 1 carried secret); body: %s", len(envs), body)
	}

	foundNew, foundSecret := false, false
	for _, e := range envs {
		if e.Name == "NEW" && e.ValueOrReferenceToSecret == "val" && e.Type == "plain" {
			foundNew = true
		}
		if e.Name == "SECRET_KEY" && e.ValueOrReferenceToSecret == "my-secret" && e.Type == "secret" {
			foundSecret = true
		}
	}
	if !foundNew {
		t.Errorf("expected NEW=val (plain) in env; body: %s", body)
	}
	if !foundSecret {
		t.Errorf("expected SECRET_KEY=my-secret (secret) carried from current; body: %s", body)
	}
	// FOO must NOT be present (it was plain and --env replaces all plain vars).
	for _, e := range envs {
		if e.Name == "FOO" {
			t.Errorf("FOO should be replaced by --env flag, but found in env; body: %s", body)
		}
	}
}

// TestContainerUpdate_ScalingWireFormat asserts that updating scaling flags sends
// a PATCH to /container-deployments/{name}/scaling (not the main deployment PATCH),
// and the deployment PATCH is NOT sent when only scaling flags are set.
func TestContainerUpdate_ScalingWireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"update", "cli-test",
		"--min-replicas", "0",
		"--max-replicas", "10",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container update failed: %v\nstderr:\n%s", err, stderr.String())
	}

	// Scaling PATCH must have been sent.
	scalingBody := rec.containerScalingPatchBody()
	if scalingBody == nil {
		t.Fatalf("server did not receive PATCH /container-deployments/cli-test/scaling\nstderr:\n%s", stderr.String())
	}

	var got struct {
		MinReplicaCount *int `json:"min_replica_count"`
		MaxReplicaCount *int `json:"max_replica_count"`
	}
	if err := json.Unmarshal(scalingBody, &got); err != nil {
		t.Fatalf("unmarshal scaling body: %v\nbody: %s", err, scalingBody)
	}
	if got.MinReplicaCount == nil || *got.MinReplicaCount != 0 {
		t.Errorf("min_replica_count: want 0, got %v", got.MinReplicaCount)
	}
	if got.MaxReplicaCount == nil || *got.MaxReplicaCount != 10 {
		t.Errorf("max_replica_count: want 10, got %v", got.MaxReplicaCount)
	}

	// Deployment PATCH must NOT have been sent (only scaling changed).
	if rec.containerPatchBody() != nil {
		t.Errorf("deployment PATCH should not be sent when only scaling flags are set; body: %s", rec.containerPatchBody())
	}
}

// TestContainerUpdate_CombinedWireFormat asserts that updating both deployment
// and scaling flags sends two PATCHes: one to /container-deployments/{name} and
// one to /container-deployments/{name}/scaling.
func TestContainerUpdate_CombinedWireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"update", "cli-test",
		"--image", "ghcr.io/org/app:v3.0",
		"--max-replicas", "10",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("container update failed: %v\nstderr:\n%s", err, stderr.String())
	}

	deployBody := rec.containerPatchBody()
	if deployBody == nil {
		t.Fatalf("server did not receive PATCH /container-deployments/cli-test")
	}
	scalingBody := rec.containerScalingPatchBody()
	if scalingBody == nil {
		t.Fatalf("server did not receive PATCH /container-deployments/cli-test/scaling")
	}

	var deployGot struct {
		Containers []struct {
			Image string `json:"image"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(deployBody, &deployGot); err != nil {
		t.Fatalf("unmarshal deploy body: %v\nbody: %s", err, deployBody)
	}
	if len(deployGot.Containers) != 1 || deployGot.Containers[0].Image != "ghcr.io/org/app:v3.0" {
		t.Errorf("deployment PATCH image: got %+v", deployGot.Containers)
	}

	var rawDeploy map[string]any
	if err := json.Unmarshal(deployBody, &rawDeploy); err != nil {
		t.Fatalf("unmarshal raw deploy: %v", err)
	}
	if _, present := rawDeploy["scaling"]; present {
		t.Errorf("deployment PATCH must not contain scaling; body: %s", deployBody)
	}

	var scalingGot struct {
		MaxReplicaCount *int `json:"max_replica_count"`
	}
	if err := json.Unmarshal(scalingBody, &scalingGot); err != nil {
		t.Fatalf("unmarshal scaling body: %v\nbody: %s", err, scalingBody)
	}
	if scalingGot.MaxReplicaCount == nil || *scalingGot.MaxReplicaCount != 10 {
		t.Errorf("scaling PATCH max_replica_count: want 10, got %v", scalingGot.MaxReplicaCount)
	}
}

// TestBatchjobUpdate_WireFormat asserts that updating scaling sends the correct
// PATCH body with only the scaling field, no containers (unchanged).
func TestBatchjobUpdate_WireFormat(t *testing.T) {
	t.Parallel()
	rec := newRecordingServer(t)
	f := newTestFactory(t, rec.srv.URL)

	var stdout, stderr bytes.Buffer
	cmd := NewCmdBatchjob(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs([]string{
		"update", "cli-test-job",
		"--deadline", "1h",
		"--max-replicas", "5",
		"--yes",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("batchjob update failed: %v\nstderr:\n%s", err, stderr.String())
	}

	body := rec.jobPatchBody()
	if body == nil {
		t.Fatalf("server did not receive PATCH /job-deployments/cli-test-job\nstderr:\n%s", stderr.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}

	// containers and compute must NOT be present (unchanged).
	if _, present := raw["containers"]; present {
		t.Errorf("containers should be omitted (not changed); body: %s", body)
	}
	if _, present := raw["compute"]; present {
		t.Errorf("compute should be omitted (not changed); body: %s", body)
	}

	var got struct {
		Scaling *struct {
			MaxReplicaCount        int `json:"max_replica_count"`
			DeadlineSeconds        int `json:"deadline_seconds"`
			QueueMessageTTLSeconds int `json:"queue_message_ttl_seconds"`
		} `json:"scaling"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal typed: %v\nbody: %s", err, body)
	}

	if got.Scaling == nil {
		t.Fatalf("scaling missing from PATCH body")
	}
	if got.Scaling.DeadlineSeconds != 3600 {
		t.Errorf("deadline_seconds: got %d, want 3600", got.Scaling.DeadlineSeconds)
	}
	if got.Scaling.MaxReplicaCount != 5 {
		t.Errorf("max_replica_count: got %d, want 5", got.Scaling.MaxReplicaCount)
	}
	// queue_message_ttl_seconds should be carried from current (300).
	if got.Scaling.QueueMessageTTLSeconds != 300 {
		t.Errorf("queue_message_ttl_seconds: got %d, want 300 (carried from current)", got.Scaling.QueueMessageTTLSeconds)
	}

	if !strings.Contains(stdout.String(), "cli-test-job") {
		t.Errorf("stdout should contain deployment name; got:\n%s", stdout.String())
	}
}
