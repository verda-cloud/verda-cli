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
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func testBatchjobUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	f := cmd.Flags()
	f.String(flagImage, "", "")
	f.String(flagCompute, "", "")
	f.Int(flagComputeSize, 0, "")
	f.String(flagRegistryCreds, "", "")
	f.Bool(flagRegistryPublic, false, "")
	f.Int(flagPort, 0, "")
	f.StringArray(flagEnv, nil, "")
	f.StringArray(flagEnvSecret, nil, "")
	f.StringArray(flagEntrypoint, nil, "")
	f.StringArray(flagCmd, nil, "")
	f.Int(flagMaxReplicas, 0, "")
	f.Duration(flagDeadline, 0, "")
	f.Duration(flagRequestTTL, 0, "")
	f.StringArray(flagSecretMount, nil, "")
	return cmd
}

func baseJobDeployment() *verda.JobDeployment {
	return &verda.JobDeployment{
		Name: "my-job",
		Containers: []verda.DeploymentContainer{{
			Name:        "worker",
			Image:       verda.ContainerImage{Image: "ghcr.io/org/embedder:v1"},
			ExposedPort: 8080,
			Env: []verda.ContainerEnvVar{
				{Name: "FOO", ValueOrReferenceToSecret: "bar", Type: envTypePlain},
				{Name: "TOKEN", ValueOrReferenceToSecret: "my-secret", Type: envTypeSecret},
			},
			VolumeMounts: []verda.ContainerVolumeMount{
				{Type: mountTypeScratch, MountPath: "/data"},
			},
		}},
		Compute: &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
		Scaling: &verda.JobScalingOptions{
			MaxReplicaCount:        3,
			DeadlineSeconds:        1800,
			QueueMessageTTLSeconds: 300,
		},
	}
}

// -- buildBatchjobUpdateRequest --

func TestBuildBatchjobUpdateRequest_ComputeChange(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagCompute, "RTX6000Ada")
	_ = cmd.Flags().Set(flagComputeSize, "2")
	opts := &batchjobUpdateOptions{Compute: "RTX6000Ada", ComputeSize: 2}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Compute == nil {
		t.Fatal("Compute should be set")
	}
	if req.Compute.Name != "RTX6000Ada" || req.Compute.Size != 2 {
		t.Errorf("Compute: got %+v", req.Compute)
	}
	if req.Containers != nil {
		t.Errorf("Containers should be nil (unchanged)")
	}
	if req.Scaling != nil {
		t.Errorf("Scaling should be nil (unchanged)")
	}
}

func TestBuildBatchjobUpdateRequest_ComputeSizeInvalid(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagComputeSize, "0")
	opts := &batchjobUpdateOptions{ComputeSize: 0}

	_, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err == nil || !strings.Contains(err.Error(), "--compute-size must be >= 1") {
		t.Fatalf("expected compute-size error, got %v", err)
	}
}

func TestBuildBatchjobUpdateRequest_ComputeSizeOnlyCarriesName(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagComputeSize, "4")
	opts := &batchjobUpdateOptions{ComputeSize: 4}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Compute == nil {
		t.Fatal("Compute should be set")
	}
	if req.Compute.Name != "RTX4500Ada" {
		t.Errorf("Compute.Name: want RTX4500Ada (carried), got %q", req.Compute.Name)
	}
	if req.Compute.Size != 4 {
		t.Errorf("Compute.Size: want 4, got %d", req.Compute.Size)
	}
}

func TestBuildBatchjobUpdateRequest_RegistryCreds(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryCreds, "my-ghcr")
	opts := &batchjobUpdateOptions{RegistryCreds: "my-ghcr"}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rs := req.ContainerRegistrySettings
	if rs == nil || !rs.IsPrivate {
		t.Fatal("expected private registry")
	}
	if rs.Credentials == nil || rs.Credentials.Name != "my-ghcr" {
		t.Errorf("creds: got %+v", rs.Credentials)
	}
}

func TestBuildBatchjobUpdateRequest_RegistryPublic(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryPublic, "true")
	opts := &batchjobUpdateOptions{RegistryPublic: true}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ContainerRegistrySettings == nil || req.ContainerRegistrySettings.IsPrivate {
		t.Fatal("expected public registry")
	}
}

func TestBuildBatchjobUpdateRequest_ContainerFlagsTriggerMerge(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/embedder:v2")
	opts := &batchjobUpdateOptions{Image: "ghcr.io/org/embedder:v2"}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Containers) != 1 {
		t.Fatalf("containers: want 1, got %d", len(req.Containers))
	}
	if req.Containers[0].Image != "ghcr.io/org/embedder:v2" {
		t.Errorf("image: got %q", req.Containers[0].Image)
	}
}

func TestBuildBatchjobUpdateRequest_ScalingFlagsTriggerMerge(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "10")
	opts := &batchjobUpdateOptions{MaxReplicas: 10}

	req, err := buildBatchjobUpdateRequest(cmd, opts, baseJobDeployment())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Scaling == nil {
		t.Fatal("Scaling should be set")
	}
	if req.Scaling.MaxReplicaCount != 10 {
		t.Errorf("MaxReplicaCount: want 10, got %d", req.Scaling.MaxReplicaCount)
	}
	if req.Scaling.DeadlineSeconds != 1800 {
		t.Errorf("DeadlineSeconds: want 1800 (carried), got %d", req.Scaling.DeadlineSeconds)
	}
}

func TestBuildBatchjobUpdateRequest_EmptyContainers(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/app:v2")
	opts := &batchjobUpdateOptions{Image: "ghcr.io/org/app:v2"}
	dep := baseJobDeployment()
	dep.Containers = nil

	_, err := buildBatchjobUpdateRequest(cmd, opts, dep)
	if err == nil || !strings.Contains(err.Error(), "no containers") {
		t.Fatalf("expected empty-containers error, got %v", err)
	}
}

// -- mergeBatchjobContainerUpdate --

func TestMergeBatchjobContainerUpdate_ImageChange(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/embedder:v2")
	opts := &batchjobUpdateOptions{Image: "ghcr.io/org/embedder:v2"}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Image != "ghcr.io/org/embedder:v2" {
		t.Errorf("image: got %q", c.Image)
	}
	if c.Name != "worker" {
		t.Errorf("name: want worker (carried), got %q", c.Name)
	}
	if c.ExposedPort != 8080 {
		t.Errorf("port: want 8080 (carried), got %d", c.ExposedPort)
	}
}

func TestMergeBatchjobContainerUpdate_LatestRejected(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "nginx:latest")
	opts := &batchjobUpdateOptions{Image: "nginx:latest"}

	_, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestMergeBatchjobContainerUpdate_PortChange(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagPort, "3000")
	opts := &batchjobUpdateOptions{Port: 3000}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ExposedPort != 3000 {
		t.Errorf("port: want 3000, got %d", c.ExposedPort)
	}
}

func TestMergeBatchjobContainerUpdate_PortOutOfRange(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagPort, "0")
	opts := &batchjobUpdateOptions{Port: 0}

	_, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err == nil || !strings.Contains(err.Error(), "--port must be in 1..65535") {
		t.Fatalf("expected port error, got %v", err)
	}
}

func TestMergeBatchjobContainerUpdate_EnvPlainOnly(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "NEW=val")
	opts := &batchjobUpdateOptions{Env: []string{"NEW=val"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Env) != 2 {
		t.Fatalf("env: want 2 (1 new plain + 1 carried secret), got %d", len(c.Env))
	}
	foundNew, foundSecret := false, false
	for _, e := range c.Env {
		if e.Name == "NEW" && e.Type == envTypePlain {
			foundNew = true
		}
		if e.Name == "TOKEN" && e.Type == envTypeSecret {
			foundSecret = true
		}
	}
	if !foundNew {
		t.Error("expected NEW=val (plain)")
	}
	if !foundSecret {
		t.Error("expected TOKEN (secret, carried)")
	}
}

func TestMergeBatchjobContainerUpdate_EnvSecretOnly(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagEnvSecret, "NEW_SECRET=new-ref")
	opts := &batchjobUpdateOptions{EnvSecret: []string{"NEW_SECRET=new-ref"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Env) != 2 {
		t.Fatalf("env: want 2 (1 carried plain + 1 new secret), got %d", len(c.Env))
	}
	foundPlain, foundNewSecret := false, false
	for _, e := range c.Env {
		if e.Name == "FOO" && e.Type == envTypePlain {
			foundPlain = true
		}
		if e.Name == "NEW_SECRET" && e.Type == envTypeSecret {
			foundNewSecret = true
		}
	}
	if !foundPlain {
		t.Error("expected FOO (plain, carried)")
	}
	if !foundNewSecret {
		t.Error("expected NEW_SECRET (secret, new)")
	}
}

func TestMergeBatchjobContainerUpdate_EnvBothReplace(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "A=1")
	_ = cmd.Flags().Set(flagEnvSecret, "B=s")
	opts := &batchjobUpdateOptions{Env: []string{"A=1"}, EnvSecret: []string{"B=s"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Env) != 2 {
		t.Fatalf("env: want 2 (both replaced), got %d", len(c.Env))
	}
	for _, e := range c.Env {
		if e.Name == "FOO" || e.Name == "TOKEN" {
			t.Errorf("old env %q should not be present", e.Name)
		}
	}
}

func TestMergeBatchjobContainerUpdate_Entrypoint(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagEntrypoint, "/bin/sh")
	opts := &batchjobUpdateOptions{Entrypoint: []string{"/bin/sh"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.EntrypointOverrides == nil || !c.EntrypointOverrides.Enabled {
		t.Fatal("expected enabled entrypoint overrides")
	}
	if len(c.EntrypointOverrides.Entrypoint) != 1 || c.EntrypointOverrides.Entrypoint[0] != "/bin/sh" {
		t.Errorf("entrypoint: got %+v", c.EntrypointOverrides)
	}
}

func TestMergeBatchjobContainerUpdate_CmdOnly(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagCmd, "--workers=4")
	opts := &batchjobUpdateOptions{Cmd: []string{"--workers=4"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.EntrypointOverrides == nil || !c.EntrypointOverrides.Enabled {
		t.Fatal("expected enabled entrypoint overrides")
	}
	if len(c.EntrypointOverrides.Cmd) != 1 || c.EntrypointOverrides.Cmd[0] != "--workers=4" {
		t.Errorf("cmd: got %+v", c.EntrypointOverrides)
	}
}

func TestMergeBatchjobContainerUpdate_SecretMount(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagSecretMount, "api-key:/etc/secret/key")
	opts := &batchjobUpdateOptions{SecretMounts: []string{"api-key:/etc/secret/key"}}

	c, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.VolumeMounts) != 2 {
		t.Fatalf("mounts: want 2 (scratch carried + secret), got %d", len(c.VolumeMounts))
	}
	if c.VolumeMounts[0].Type != mountTypeScratch {
		t.Errorf("mount[0]: want scratch, got %q", c.VolumeMounts[0].Type)
	}
	if c.VolumeMounts[1].Type != mountTypeSecret || c.VolumeMounts[1].SecretName != "api-key" {
		t.Errorf("mount[1]: got %+v", c.VolumeMounts[1])
	}
}

func TestMergeBatchjobContainerUpdate_SecretMountInvalid(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagSecretMount, "bad-format")
	opts := &batchjobUpdateOptions{SecretMounts: []string{"bad-format"}}

	_, err := mergeBatchjobContainerUpdate(cmd, opts, &baseJobDeployment().Containers[0])
	if err == nil || !strings.Contains(err.Error(), "expected SECRET:MOUNT_PATH") {
		t.Fatalf("expected secret mount parse error, got %v", err)
	}
}

// -- mergeBatchjobScalingUpdate --

func TestMergeBatchjobScalingUpdate_MaxReplicas(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "10")
	opts := &batchjobUpdateOptions{MaxReplicas: 10}

	got, err := mergeBatchjobScalingUpdate(cmd, opts, baseJobDeployment().Scaling)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxReplicaCount != 10 {
		t.Errorf("MaxReplicaCount: want 10, got %d", got.MaxReplicaCount)
	}
	if got.DeadlineSeconds != 1800 {
		t.Errorf("DeadlineSeconds: want 1800 (carried), got %d", got.DeadlineSeconds)
	}
	if got.QueueMessageTTLSeconds != 300 {
		t.Errorf("QueueMessageTTLSeconds: want 300 (carried), got %d", got.QueueMessageTTLSeconds)
	}
}

func TestMergeBatchjobScalingUpdate_MaxReplicasInvalid(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "0")
	opts := &batchjobUpdateOptions{MaxReplicas: 0}

	_, err := mergeBatchjobScalingUpdate(cmd, opts, baseJobDeployment().Scaling)
	if err == nil || !strings.Contains(err.Error(), "--max-replicas must be >= 1") {
		t.Fatalf("expected max-replicas error, got %v", err)
	}
}

func TestMergeBatchjobScalingUpdate_Deadline(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagDeadline, "1h")
	opts := &batchjobUpdateOptions{Deadline: time.Hour}

	got, err := mergeBatchjobScalingUpdate(cmd, opts, baseJobDeployment().Scaling)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DeadlineSeconds != 3600 {
		t.Errorf("DeadlineSeconds: want 3600, got %d", got.DeadlineSeconds)
	}
}

func TestMergeBatchjobScalingUpdate_DeadlineInvalid(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagDeadline, "-1s")
	opts := &batchjobUpdateOptions{Deadline: -1 * time.Second}

	_, err := mergeBatchjobScalingUpdate(cmd, opts, baseJobDeployment().Scaling)
	if err == nil || !strings.Contains(err.Error(), "--deadline must be > 0") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestMergeBatchjobScalingUpdate_RequestTTL(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagRequestTTL, "10m")
	opts := &batchjobUpdateOptions{RequestTTL: 10 * time.Minute}

	got, err := mergeBatchjobScalingUpdate(cmd, opts, baseJobDeployment().Scaling)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.QueueMessageTTLSeconds != 600 {
		t.Errorf("QueueMessageTTLSeconds: want 600, got %d", got.QueueMessageTTLSeconds)
	}
}

func TestMergeBatchjobScalingUpdate_NilCurrent(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "5")
	_ = cmd.Flags().Set(flagDeadline, "30m")
	opts := &batchjobUpdateOptions{MaxReplicas: 5, Deadline: 30 * time.Minute}

	got, err := mergeBatchjobScalingUpdate(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxReplicaCount != 5 {
		t.Errorf("MaxReplicaCount: want 5, got %d", got.MaxReplicaCount)
	}
	if got.DeadlineSeconds != 1800 {
		t.Errorf("DeadlineSeconds: want 1800, got %d", got.DeadlineSeconds)
	}
	if got.QueueMessageTTLSeconds != 0 {
		t.Errorf("QueueMessageTTLSeconds: want 0 (zero-value), got %d", got.QueueMessageTTLSeconds)
	}
}

// -- renderBatchjobUpdateSummary --

func TestRenderBatchjobUpdateSummary(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/embedder:v2")
	_ = cmd.Flags().Set(flagCompute, "RTX6000Ada")
	_ = cmd.Flags().Set(flagComputeSize, "2")
	_ = cmd.Flags().Set(flagPort, "3000")
	_ = cmd.Flags().Set(flagMaxReplicas, "10")
	_ = cmd.Flags().Set(flagDeadline, "1h")
	_ = cmd.Flags().Set(flagRequestTTL, "5m")

	opts := &batchjobUpdateOptions{
		Image:       "ghcr.io/org/embedder:v2",
		Compute:     "RTX6000Ada",
		ComputeSize: 2,
		Port:        3000,
		MaxReplicas: 10,
		Deadline:    time.Hour,
		RequestTTL:  5 * time.Minute,
	}

	var buf bytes.Buffer
	renderBatchjobUpdateSummary(&buf, "my-job", cmd, opts, baseJobDeployment())
	out := buf.String()

	for _, want := range []string{
		"Updating", "my-job",
		"ghcr.io/org/embedder:v2",
		"RTX6000Ada",
		"3000",
		"10",
		"1h0m0s",
		"5m0s",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q;\ngot:\n%s", want, out)
		}
	}
}

func TestRenderBatchjobUpdateSummary_RegistryFields(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryCreds, "my-ghcr")
	opts := &batchjobUpdateOptions{RegistryCreds: "my-ghcr"}

	var buf bytes.Buffer
	renderBatchjobUpdateSummary(&buf, "j", cmd, opts, baseJobDeployment())
	if !strings.Contains(buf.String(), "my-ghcr") {
		t.Errorf("expected creds name in output; got:\n%s", buf.String())
	}
}

func TestRenderBatchjobUpdateSummary_RegistryPublic(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryPublic, "true")
	opts := &batchjobUpdateOptions{RegistryPublic: true}

	var buf bytes.Buffer
	renderBatchjobUpdateSummary(&buf, "j", cmd, opts, baseJobDeployment())
	if !strings.Contains(buf.String(), "public") {
		t.Errorf("expected 'public'; got:\n%s", buf.String())
	}
}

func TestRenderBatchjobUpdateSummary_EnvCounts(t *testing.T) {
	cmd := testBatchjobUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "A=1")
	_ = cmd.Flags().Set(flagEnvSecret, "B=s")
	_ = cmd.Flags().Set(flagSecretMount, "k:/p")
	opts := &batchjobUpdateOptions{
		Env:          []string{"A=1"},
		EnvSecret:    []string{"B=s"},
		SecretMounts: []string{"k:/p"},
	}

	var buf bytes.Buffer
	renderBatchjobUpdateSummary(&buf, "j", cmd, opts, baseJobDeployment())
	out := buf.String()
	if !strings.Contains(out, "Env") || !strings.Contains(out, "Secret") {
		t.Errorf("expected env/secret labels in output; got:\n%s", out)
	}
}
