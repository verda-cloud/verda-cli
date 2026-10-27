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

// testContainerUpdateCmd creates a cobra command with all container-update
// flags registered so cmd.Flags().Set marks them Changed.
func testContainerUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	f := cmd.Flags()
	f.String(flagImage, "", "")
	f.Bool(flagSpot, false, "")
	f.Bool(flagNoSpot, false, "")
	f.String(flagCompute, "", "")
	f.Int(flagComputeSize, 0, "")
	f.String(flagRegistryCreds, "", "")
	f.Bool(flagRegistryPublic, false, "")
	f.Int(flagPort, 0, "")
	f.Bool(flagHealthcheckOff, false, "")
	f.Int(flagHealthcheckPort, 0, "")
	f.String(flagHealthcheckPath, "", "")
	f.StringArray(flagEnv, nil, "")
	f.StringArray(flagEnvSecret, nil, "")
	f.StringArray(flagEntrypoint, nil, "")
	f.StringArray(flagCmd, nil, "")
	f.StringArray(flagSecretMount, nil, "")
	f.Int(flagMinReplicas, 0, "")
	f.Int(flagMaxReplicas, 0, "")
	f.Int(flagConcurrency, 0, "")
	f.String(flagQueuePreset, "", "")
	f.Int(flagQueueLoad, 0, "")
	f.Int(flagCPUUtil, 0, "")
	f.Int(flagGPUUtil, 0, "")
	f.Duration(flagScaleUpDelay, 0, "")
	f.Duration(flagScaleDownDelay, 0, "")
	f.Duration(flagRequestTTL, 0, "")
	return cmd
}

func baseContainerDeployment() *verda.ContainerDeployment {
	return &verda.ContainerDeployment{
		Name: "my-endpoint",
		Containers: []verda.DeploymentContainer{{
			Name:        "main",
			Image:       verda.ContainerImage{Image: "ghcr.io/org/app:v1.0"},
			ExposedPort: 8080,
			Healthcheck: &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"},
			Env: []verda.ContainerEnvVar{
				{Name: "FOO", ValueOrReferenceToSecret: "bar", Type: envTypePlain},
				{Name: "SECRET_KEY", ValueOrReferenceToSecret: "my-secret", Type: envTypeSecret},
			},
			VolumeMounts: []verda.ContainerVolumeMount{
				{Type: mountTypeScratch, MountPath: "/data"},
			},
		}},
		Compute: &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
	}
}

func baseContainerScaling() *verda.ContainerScalingOptions {
	return &verda.ContainerScalingOptions{
		MinReplicaCount:              1,
		MaxReplicaCount:              3,
		ConcurrentRequestsPerReplica: 1,
		QueueMessageTTLSeconds:       300,
		ScalingTriggers: &verda.ScalingTriggers{
			QueueLoad: &verda.QueueLoadTrigger{Threshold: 3},
		},
	}
}

// -- buildContainerUpdateRequest --

func TestBuildContainerUpdateRequest_SpotOn(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagSpot, "true")
	opts := &containerUpdateOptions{Spot: true}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.IsSpot == nil || !*req.IsSpot {
		t.Errorf("IsSpot: want true, got %v", req.IsSpot)
	}
	if req.Compute != nil {
		t.Errorf("Compute should be nil (unchanged)")
	}
	if req.Scaling != nil {
		t.Errorf("Scaling should be nil (unchanged)")
	}
}

func TestBuildContainerUpdateRequest_NoSpot(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagNoSpot, "true")
	opts := &containerUpdateOptions{NoSpot: true}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.IsSpot == nil || *req.IsSpot {
		t.Errorf("IsSpot: want false, got %v", req.IsSpot)
	}
}

func TestBuildContainerUpdateRequest_ComputeSizeOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagComputeSize, "2")
	opts := &containerUpdateOptions{ComputeSize: 2}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Compute == nil {
		t.Fatal("Compute should be set")
	}
	if req.Compute.Name != "RTX4500Ada" {
		t.Errorf("Compute.Name: want RTX4500Ada (carried), got %q", req.Compute.Name)
	}
	if req.Compute.Size != 2 {
		t.Errorf("Compute.Size: want 2, got %d", req.Compute.Size)
	}
}

func TestBuildContainerUpdateRequest_ComputeNameOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCompute, "RTX6000Ada")
	opts := &containerUpdateOptions{Compute: "RTX6000Ada"}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Compute == nil {
		t.Fatal("Compute should be set")
	}
	if req.Compute.Name != "RTX6000Ada" {
		t.Errorf("Compute.Name: want RTX6000Ada, got %q", req.Compute.Name)
	}
	if req.Compute.Size != 1 {
		t.Errorf("Compute.Size: want 1 (carried), got %d", req.Compute.Size)
	}
}

func TestBuildContainerUpdateRequest_ComputeSizeInvalid(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagComputeSize, "0")
	opts := &containerUpdateOptions{ComputeSize: 0}

	_, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err == nil || !strings.Contains(err.Error(), "--compute-size must be >= 1") {
		t.Fatalf("expected compute-size error, got %v", err)
	}
}

func TestBuildContainerUpdateRequest_NilCurrentCompute(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCompute, "RTX6000Ada")
	opts := &containerUpdateOptions{Compute: "RTX6000Ada"}
	dep := baseContainerDeployment()
	dep.Compute = nil

	req, err := buildContainerUpdateRequest(cmd, opts, dep, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Compute.Name != "RTX6000Ada" {
		t.Errorf("Compute.Name: want RTX6000Ada, got %q", req.Compute.Name)
	}
	if req.Compute.Size != 0 {
		t.Errorf("Compute.Size: want 0 (nil current), got %d", req.Compute.Size)
	}
}

func TestBuildContainerUpdateRequest_RegistryCreds(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryCreds, "my-ghcr")
	opts := &containerUpdateOptions{RegistryCreds: "my-ghcr"}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ContainerRegistrySettings == nil || !req.ContainerRegistrySettings.IsPrivate {
		t.Fatal("expected private registry settings")
	}
	if req.ContainerRegistrySettings.Credentials == nil || req.ContainerRegistrySettings.Credentials.Name != "my-ghcr" {
		t.Errorf("creds name: got %+v", req.ContainerRegistrySettings.Credentials)
	}
}

func TestBuildContainerUpdateRequest_RegistryPublic(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryPublic, "true")
	opts := &containerUpdateOptions{RegistryPublic: true}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ContainerRegistrySettings == nil || req.ContainerRegistrySettings.IsPrivate {
		t.Fatal("expected public registry settings")
	}
}

func TestBuildContainerUpdateRequest_ImageChange(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/app:v2.0")
	opts := &containerUpdateOptions{Image: "ghcr.io/org/app:v2.0"}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Containers) != 1 {
		t.Fatalf("containers: want 1, got %d", len(req.Containers))
	}
	if req.Containers[0].Image != "ghcr.io/org/app:v2.0" {
		t.Errorf("image: got %q", req.Containers[0].Image)
	}
	if req.Containers[0].ExposedPort != 8080 {
		t.Errorf("port: want 8080 (carried), got %d", req.Containers[0].ExposedPort)
	}
}

func TestBuildContainerUpdateRequest_LatestTagRejected(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/app:latest")
	opts := &containerUpdateOptions{Image: "ghcr.io/org/app:latest"}

	_, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err == nil || !strings.Contains(err.Error(), "latest") {
		t.Fatalf("expected :latest rejection, got %v", err)
	}
}

func TestBuildContainerUpdateRequest_PortOutOfRange(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagPort, "99999")
	opts := &containerUpdateOptions{Port: 99999}

	_, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), nil)
	if err == nil || !strings.Contains(err.Error(), "--port must be in 1..65535") {
		t.Fatalf("expected port error, got %v", err)
	}
}

func TestBuildContainerUpdateRequest_EmptyContainers(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/app:v2.0")
	opts := &containerUpdateOptions{Image: "ghcr.io/org/app:v2.0"}
	dep := baseContainerDeployment()
	dep.Containers = nil

	_, err := buildContainerUpdateRequest(cmd, opts, dep, nil)
	if err == nil || !strings.Contains(err.Error(), "no containers") {
		t.Fatalf("expected empty-containers error, got %v", err)
	}
}

func TestBuildContainerUpdateRequest_ScalingOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "10")
	opts := &containerUpdateOptions{MaxReplicas: 10}

	req, err := buildContainerUpdateRequest(cmd, opts, baseContainerDeployment(), baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Containers != nil {
		t.Errorf("Containers should be nil (unchanged)")
	}
	if req.Scaling == nil {
		t.Fatal("Scaling should be set")
	}
	if req.Scaling.MaxReplicaCount != 10 {
		t.Errorf("MaxReplicaCount: want 10, got %d", req.Scaling.MaxReplicaCount)
	}
	if req.Scaling.MinReplicaCount != 1 {
		t.Errorf("MinReplicaCount: want 1 (carried), got %d", req.Scaling.MinReplicaCount)
	}
}

// -- mergeHealthcheckUpdate --

func TestMergeHealthcheckUpdate_NoFlags(t *testing.T) {
	cmd := testContainerUpdateCmd()
	opts := &containerUpdateOptions{}
	current := &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"}

	got := mergeHealthcheckUpdate(cmd, opts, current, 8080)
	if got != current {
		t.Errorf("expected same pointer back when no flags changed")
	}
}

func TestMergeHealthcheckUpdate_Off(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckOff, "true")
	opts := &containerUpdateOptions{HealthcheckOff: true}
	current := &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"}

	got := mergeHealthcheckUpdate(cmd, opts, current, 8080)
	if got != nil {
		t.Errorf("expected nil when healthcheck-off, got %+v", got)
	}
}

func TestMergeHealthcheckUpdate_PathOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckPath, "/ready")
	opts := &containerUpdateOptions{HealthcheckPath: "/ready"}
	current := &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"}

	got := mergeHealthcheckUpdate(cmd, opts, current, 8080)
	if got == nil || !got.Enabled {
		t.Fatalf("expected enabled healthcheck, got %+v", got)
	}
	if got.Port != 8080 {
		t.Errorf("port: want 8080 (carried), got %d", got.Port)
	}
	if got.Path != "/ready" {
		t.Errorf("path: want /ready, got %q", got.Path)
	}
}

func TestMergeHealthcheckUpdate_PortOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckPort, "9090")
	opts := &containerUpdateOptions{HealthcheckPort: 9090}
	current := &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"}

	got := mergeHealthcheckUpdate(cmd, opts, current, 8080)
	if got == nil || !got.Enabled {
		t.Fatalf("expected enabled healthcheck, got %+v", got)
	}
	if got.Port != 9090 {
		t.Errorf("port: want 9090, got %d", got.Port)
	}
	if got.Path != "/health" {
		t.Errorf("path: want /health (carried), got %q", got.Path)
	}
}

func TestMergeHealthcheckUpdate_BothPortAndPath(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckPort, "9090")
	_ = cmd.Flags().Set(flagHealthcheckPath, "/ready")
	opts := &containerUpdateOptions{HealthcheckPort: 9090, HealthcheckPath: "/ready"}

	got := mergeHealthcheckUpdate(cmd, opts, nil, 8080)
	if got == nil || !got.Enabled {
		t.Fatalf("expected enabled healthcheck, got %+v", got)
	}
	if got.Port != 9090 {
		t.Errorf("port: want 9090, got %d", got.Port)
	}
	if got.Path != "/ready" {
		t.Errorf("path: want /ready, got %q", got.Path)
	}
}

func TestMergeHealthcheckUpdate_NilCurrent_FallbackToExposedPort(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckPath, "/health")
	opts := &containerUpdateOptions{HealthcheckPath: "/health"}

	got := mergeHealthcheckUpdate(cmd, opts, nil, 3000)
	if got == nil || !got.Enabled {
		t.Fatalf("expected enabled healthcheck, got %+v", got)
	}
	if got.Port != 3000 {
		t.Errorf("port: want 3000 (fallback to exposed), got %d", got.Port)
	}
}

func TestMergeHealthcheckUpdate_PortZero_FallbackToExposedPort(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagHealthcheckPort, "0")
	opts := &containerUpdateOptions{HealthcheckPort: 0}

	got := mergeHealthcheckUpdate(cmd, opts, nil, 8080)
	if got == nil {
		t.Fatal("expected non-nil healthcheck")
	}
	if got.Port != 8080 {
		t.Errorf("port: want 8080 (fallback), got %d", got.Port)
	}
}

// -- mergeContainerScalingUpdate --

func TestMergeContainerScalingUpdate_MinReplicas(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMinReplicas, "2")
	opts := &containerUpdateOptions{MinReplicas: 2}

	got, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MinReplicaCount != 2 {
		t.Errorf("MinReplicaCount: want 2, got %d", got.MinReplicaCount)
	}
	if got.MaxReplicaCount != 3 {
		t.Errorf("MaxReplicaCount: want 3 (carried), got %d", got.MaxReplicaCount)
	}
	if got.QueueMessageTTLSeconds != 300 {
		t.Errorf("QueueMessageTTLSeconds: want 300 (carried), got %d", got.QueueMessageTTLSeconds)
	}
}

func TestMergeContainerScalingUpdate_MaxLessThanMin(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "1")
	opts := &containerUpdateOptions{MaxReplicas: 1}
	current := &verda.ContainerScalingOptions{MinReplicaCount: 2, MaxReplicaCount: 5}

	_, err := mergeContainerScalingUpdate(cmd, opts, current)
	if err == nil || !strings.Contains(err.Error(), "--max-replicas must be >= --min-replicas") {
		t.Fatalf("expected max<min error, got %v", err)
	}
}

func TestMergeContainerScalingUpdate_Concurrency(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagConcurrency, "4")
	opts := &containerUpdateOptions{Concurrency: 4}

	got, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ConcurrentRequestsPerReplica != 4 {
		t.Errorf("Concurrency: want 4, got %d", got.ConcurrentRequestsPerReplica)
	}
}

func TestMergeContainerScalingUpdate_ConcurrencyInvalid(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagConcurrency, "0")
	opts := &containerUpdateOptions{Concurrency: 0}

	_, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err == nil || !strings.Contains(err.Error(), "--concurrency must be >= 1") {
		t.Fatalf("expected concurrency error, got %v", err)
	}
}

func TestMergeContainerScalingUpdate_QueuePreset(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagQueuePreset, presetInstant)
	opts := &containerUpdateOptions{QueuePreset: presetInstant}

	got, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ScalingTriggers == nil || got.ScalingTriggers.QueueLoad == nil {
		t.Fatal("expected QueueLoad trigger")
	}
	if got.ScalingTriggers.QueueLoad.Threshold != queueLoadInstant {
		t.Errorf("threshold: want %v, got %v", queueLoadInstant, got.ScalingTriggers.QueueLoad.Threshold)
	}
}

func TestMergeContainerScalingUpdate_ScaleDelays(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagScaleUpDelay, "30s")
	_ = cmd.Flags().Set(flagScaleDownDelay, "60s")
	opts := &containerUpdateOptions{
		ScaleUpDelay:   30 * time.Second,
		ScaleDownDelay: 60 * time.Second,
	}

	got, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ScaleUpPolicy == nil || got.ScaleUpPolicy.DelaySeconds != 30 {
		t.Errorf("ScaleUpPolicy: want 30s, got %+v", got.ScaleUpPolicy)
	}
	if got.ScaleDownPolicy == nil || got.ScaleDownPolicy.DelaySeconds != 60 {
		t.Errorf("ScaleDownPolicy: want 60s, got %+v", got.ScaleDownPolicy)
	}
}

func TestMergeContainerScalingUpdate_RequestTTL(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagRequestTTL, "10m")
	opts := &containerUpdateOptions{RequestTTL: 10 * time.Minute}

	got, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.QueueMessageTTLSeconds != 600 {
		t.Errorf("QueueMessageTTLSeconds: want 600, got %d", got.QueueMessageTTLSeconds)
	}
}

func TestMergeContainerScalingUpdate_NilCurrent(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "5")
	opts := &containerUpdateOptions{MaxReplicas: 5}

	got, err := mergeContainerScalingUpdate(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.MaxReplicaCount != 5 {
		t.Errorf("MaxReplicaCount: want 5, got %d", got.MaxReplicaCount)
	}
	if got.MinReplicaCount != 0 {
		t.Errorf("MinReplicaCount: want 0 (zero-value), got %d", got.MinReplicaCount)
	}
}

func TestMergeContainerScalingUpdate_MinReplicasNegative(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMinReplicas, "-1")
	opts := &containerUpdateOptions{MinReplicas: -1}

	_, err := mergeContainerScalingUpdate(cmd, opts, baseContainerScaling())
	if err == nil || !strings.Contains(err.Error(), "--min-replicas must be >= 0") {
		t.Fatalf("expected min-replicas error, got %v", err)
	}
}

func TestMergeContainerScalingUpdate_MaxReplicasInvalid(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagMaxReplicas, "0")
	opts := &containerUpdateOptions{MaxReplicas: 0}

	_, err := mergeContainerScalingUpdate(cmd, opts, nil)
	if err == nil || !strings.Contains(err.Error(), "--max-replicas must be >= 1") {
		t.Fatalf("expected max-replicas error, got %v", err)
	}
}

// -- mergeScalingTriggers --

func TestMergeScalingTriggers_QueuePreset(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagQueuePreset, presetCostSaver)
	opts := &containerUpdateOptions{QueuePreset: presetCostSaver}

	got, err := mergeScalingTriggers(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.QueueLoad == nil || got.QueueLoad.Threshold != queueLoadCostSaver {
		t.Errorf("QueueLoad: want %v, got %+v", queueLoadCostSaver, got.QueueLoad)
	}
}

func TestMergeScalingTriggers_CPUUtilEnable(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCPUUtil, "70")
	opts := &containerUpdateOptions{CPUUtil: 70}

	got, err := mergeScalingTriggers(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CPUUtilization == nil || !got.CPUUtilization.Enabled || got.CPUUtilization.Threshold != 70 {
		t.Errorf("CPUUtilization: want enabled@70, got %+v", got.CPUUtilization)
	}
}

func TestMergeScalingTriggers_CPUUtilDisable(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCPUUtil, "0")
	opts := &containerUpdateOptions{CPUUtil: 0}
	current := &verda.ScalingTriggers{
		CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
	}

	got, err := mergeScalingTriggers(cmd, opts, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CPUUtilization != nil {
		t.Errorf("CPUUtilization: want nil (disabled), got %+v", got.CPUUtilization)
	}
}

func TestMergeScalingTriggers_CPUUtilOutOfRange(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCPUUtil, "101")
	opts := &containerUpdateOptions{CPUUtil: 101}

	_, err := mergeScalingTriggers(cmd, opts, nil)
	if err == nil || !strings.Contains(err.Error(), "--cpu-util must be in 0..100") {
		t.Fatalf("expected cpu-util error, got %v", err)
	}
}

func TestMergeScalingTriggers_GPUUtilEnable(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagGPUUtil, "80")
	opts := &containerUpdateOptions{GPUUtil: 80}

	got, err := mergeScalingTriggers(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GPUUtilization == nil || !got.GPUUtilization.Enabled || got.GPUUtilization.Threshold != 80 {
		t.Errorf("GPUUtilization: want enabled@80, got %+v", got.GPUUtilization)
	}
}

func TestMergeScalingTriggers_GPUUtilDisable(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagGPUUtil, "0")
	opts := &containerUpdateOptions{GPUUtil: 0}
	current := &verda.ScalingTriggers{
		GPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 80},
	}

	got, err := mergeScalingTriggers(cmd, opts, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GPUUtilization != nil {
		t.Errorf("GPUUtilization: want nil (disabled), got %+v", got.GPUUtilization)
	}
}

func TestMergeScalingTriggers_GPUUtilOutOfRange(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagGPUUtil, "101")
	opts := &containerUpdateOptions{GPUUtil: 101}

	_, err := mergeScalingTriggers(cmd, opts, nil)
	if err == nil || !strings.Contains(err.Error(), "--gpu-util must be in 0..100") {
		t.Fatalf("expected gpu-util error, got %v", err)
	}
}

func TestMergeScalingTriggers_CarriesCurrent(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCPUUtil, "70")
	opts := &containerUpdateOptions{CPUUtil: 70}
	current := &verda.ScalingTriggers{
		QueueLoad:      &verda.QueueLoadTrigger{Threshold: 3},
		GPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 80},
	}

	got, err := mergeScalingTriggers(cmd, opts, current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.QueueLoad == nil || got.QueueLoad.Threshold != 3 {
		t.Errorf("QueueLoad should be carried; got %+v", got.QueueLoad)
	}
	if got.GPUUtilization == nil || got.GPUUtilization.Threshold != 80 {
		t.Errorf("GPUUtilization should be carried; got %+v", got.GPUUtilization)
	}
}

func TestMergeScalingTriggers_CustomQueueLoad(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagQueueLoad, "42")
	opts := &containerUpdateOptions{QueueLoad: 42}

	got, err := mergeScalingTriggers(cmd, opts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.QueueLoad == nil || got.QueueLoad.Threshold != 42 {
		t.Errorf("QueueLoad: want 42, got %+v", got.QueueLoad)
	}
}

// -- renderContainerUpdateSummary --

// -- mergeContainerUpdate (env, entrypoint, secret-mount branches) --

func TestMergeContainerUpdate_EnvPlainOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "NEW=val")
	opts := &containerUpdateOptions{Env: []string{"NEW=val"}}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
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
		if e.Name == "SECRET_KEY" && e.Type == envTypeSecret {
			foundSecret = true
		}
	}
	if !foundNew {
		t.Error("expected NEW=val (plain)")
	}
	if !foundSecret {
		t.Error("expected SECRET_KEY (secret, carried)")
	}
}

func TestMergeContainerUpdate_EnvSecretOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagEnvSecret, "NEW_SECRET=ref")
	opts := &containerUpdateOptions{EnvSecret: []string{"NEW_SECRET=ref"}}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
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

func TestMergeContainerUpdate_EnvBadFormat(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "BAD")
	opts := &containerUpdateOptions{Env: []string{"BAD"}}

	_, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
	if err == nil || !strings.Contains(err.Error(), "expected KEY=VALUE") {
		t.Fatalf("expected env parse error, got %v", err)
	}
}

func TestMergeContainerUpdate_Entrypoint(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagEntrypoint, "/bin/sh")
	opts := &containerUpdateOptions{Entrypoint: []string{"/bin/sh"}}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
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

func TestMergeContainerUpdate_CmdOnly(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagCmd, "--serve")
	opts := &containerUpdateOptions{Cmd: []string{"--serve"}}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.EntrypointOverrides == nil || !c.EntrypointOverrides.Enabled {
		t.Fatal("expected enabled entrypoint overrides")
	}
	if len(c.EntrypointOverrides.Cmd) != 1 || c.EntrypointOverrides.Cmd[0] != "--serve" {
		t.Errorf("cmd: got %+v", c.EntrypointOverrides)
	}
}

func TestMergeContainerUpdate_SecretMount(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagSecretMount, "api-key:/etc/secret/key")
	opts := &containerUpdateOptions{SecretMounts: []string{"api-key:/etc/secret/key"}}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
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

func TestMergeContainerUpdate_SecretMountBadFormat(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagSecretMount, "bad-format")
	opts := &containerUpdateOptions{SecretMounts: []string{"bad-format"}}

	_, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
	if err == nil || !strings.Contains(err.Error(), "expected SECRET:MOUNT_PATH") {
		t.Fatalf("expected secret mount parse error, got %v", err)
	}
}

func TestMergeContainerUpdate_PortValid(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagPort, "3000")
	opts := &containerUpdateOptions{Port: 3000}

	c, err := mergeContainerUpdate(cmd, opts, &baseContainerDeployment().Containers[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ExposedPort != 3000 {
		t.Errorf("port: want 3000, got %d", c.ExposedPort)
	}
}

// -- renderContainerUpdateSummary --

func TestRenderContainerUpdateSummary(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagImage, "ghcr.io/org/app:v2.0")
	_ = cmd.Flags().Set(flagSpot, "true")
	_ = cmd.Flags().Set(flagCompute, "RTX6000Ada")
	_ = cmd.Flags().Set(flagComputeSize, "2")
	_ = cmd.Flags().Set(flagPort, "3000")
	_ = cmd.Flags().Set(flagHealthcheckOff, "true")
	_ = cmd.Flags().Set(flagMinReplicas, "1")
	_ = cmd.Flags().Set(flagMaxReplicas, "10")
	_ = cmd.Flags().Set(flagConcurrency, "4")
	_ = cmd.Flags().Set(flagQueuePreset, "instant")
	_ = cmd.Flags().Set(flagCPUUtil, "70")
	_ = cmd.Flags().Set(flagGPUUtil, "80")
	_ = cmd.Flags().Set(flagScaleUpDelay, "30s")
	_ = cmd.Flags().Set(flagScaleDownDelay, "60s")
	_ = cmd.Flags().Set(flagRequestTTL, "5m")

	opts := &containerUpdateOptions{
		Image:          "ghcr.io/org/app:v2.0",
		Spot:           true,
		Compute:        "RTX6000Ada",
		ComputeSize:    2,
		Port:           3000,
		HealthcheckOff: true,
		MinReplicas:    1,
		MaxReplicas:    10,
		Concurrency:    4,
		QueuePreset:    "instant",
		CPUUtil:        70,
		GPUUtil:        80,
		ScaleUpDelay:   30 * time.Second,
		ScaleDownDelay: 60 * time.Second,
		RequestTTL:     5 * time.Minute,
	}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "my-endpoint", cmd, opts, baseContainerDeployment())
	out := buf.String()

	for _, want := range []string{
		"Updating", "my-endpoint",
		"ghcr.io/org/app:v2.0",
		"spot",
		"RTX6000Ada",
		"3000",
		"disabled",
		"1",   // min replicas
		"10",  // max replicas
		"4",   // concurrency
		"70%", // cpu-util
		"80%", // gpu-util
		"30s", // scale-up delay
		"1m0s",
		"5m0s", // request TTL
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q;\ngot:\n%s", want, out)
		}
	}
}

func TestRenderContainerUpdateSummary_RegistryPublic(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryPublic, "true")
	opts := &containerUpdateOptions{RegistryPublic: true}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "ep", cmd, opts, baseContainerDeployment())
	if !strings.Contains(buf.String(), "public") {
		t.Errorf("expected 'public' in output; got:\n%s", buf.String())
	}
}

func TestRenderContainerUpdateSummary_EnvCounts(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagEnv, "A=1")
	_ = cmd.Flags().Set(flagEnvSecret, "B=s")
	_ = cmd.Flags().Set(flagSecretMount, "k:/p")
	opts := &containerUpdateOptions{
		Env:          []string{"A=1"},
		EnvSecret:    []string{"B=s"},
		SecretMounts: []string{"k:/p"},
	}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "ep", cmd, opts, baseContainerDeployment())
	out := buf.String()
	if !strings.Contains(out, "1") {
		t.Errorf("expected env/secret counts in output; got:\n%s", out)
	}
}

func TestRenderContainerUpdateSummary_NoSpot(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagNoSpot, "true")
	opts := &containerUpdateOptions{NoSpot: true}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "ep", cmd, opts, baseContainerDeployment())
	if !strings.Contains(buf.String(), computeTypeOnDemand) {
		t.Errorf("expected on-demand billing label; got:\n%s", buf.String())
	}
}

func TestRenderContainerUpdateSummary_QueueLoadCustom(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagQueueLoad, "42")
	opts := &containerUpdateOptions{QueueLoad: 42}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "ep", cmd, opts, baseContainerDeployment())
	if !strings.Contains(buf.String(), "42") {
		t.Errorf("expected custom queue-load value; got:\n%s", buf.String())
	}
}

func TestRenderContainerUpdateSummary_RegistryCreds(t *testing.T) {
	cmd := testContainerUpdateCmd()
	_ = cmd.Flags().Set(flagRegistryCreds, "my-ghcr")
	opts := &containerUpdateOptions{RegistryCreds: "my-ghcr"}

	var buf bytes.Buffer
	renderContainerUpdateSummary(&buf, "ep", cmd, opts, baseContainerDeployment())
	if !strings.Contains(buf.String(), "my-ghcr") {
		t.Errorf("expected creds name in output; got:\n%s", buf.String())
	}
}
