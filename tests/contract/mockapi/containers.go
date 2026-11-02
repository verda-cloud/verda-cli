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

package mockapi

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// SeededEnvSecretValue is the env var value every seeded deployment carries;
// tests assert it never leaves a tool that must redact env values.
const SeededEnvSecretValue = "mock-env-value-must-not-leak" //nolint:gosec // G101: fixture, not a credential

type containerFixture struct {
	deployment verda.ContainerDeployment
	status     string
	scaling    verda.ContainerScalingOptions
	replicas   []verda.ReplicaInfo
}

func (s *Server) registerContainerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /container-deployments", s.handleListContainers)
	mux.HandleFunc("GET /container-deployments/{name}", s.handleGetContainer)
	mux.HandleFunc("GET /container-deployments/{name}/status", s.handleContainerStatus)
	mux.HandleFunc("GET /container-deployments/{name}/scaling", s.handleGetContainerScaling)
	mux.HandleFunc("PATCH /container-deployments/{name}/scaling", s.handlePatchContainerScaling)
	mux.HandleFunc("GET /container-deployments/{name}/replicas", s.handleContainerReplicas)
	mux.HandleFunc("POST /container-deployments/{name}/{action}", s.handleContainerAction)
}

// SeedContainerDeployment stores a healthy deployment with one running
// replica, the queue and CPU triggers enabled, the GPU trigger disabled (read
// back as production does: threshold 0), and one env var whose value is
// SeededEnvSecretValue.
func (s *Server) SeedContainerDeployment(name string) verda.ContainerDeployment {
	s.mu.Lock()
	defer s.mu.Unlock()
	dep := verda.ContainerDeployment{
		Name:            name,
		EndpointBaseURL: "https://containers.datacrunch.io/" + name,
		CreatedAt:       time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		Compute:         &verda.ContainerCompute{Name: "H100", Size: 1},
		Containers: []verda.DeploymentContainer{{
			Name:         name + "-0",
			Image:        verda.ContainerImage{Image: "docker.io/library/nginx:1.27"},
			ExposedPort:  8080,
			Healthcheck:  &verda.ContainerHealthcheck{Enabled: true, Port: 8080, Path: "/health"},
			Env:          []verda.ContainerEnvVar{{Name: "API_TOKEN", ValueOrReferenceToSecret: SeededEnvSecretValue, Type: "plain"}},
			VolumeMounts: []verda.ContainerVolumeMount{{Type: "scratch", MountPath: "/data"}},
		}},
	}
	s.containers[name] = &containerFixture{
		deployment: dep,
		status:     verda.ContainerDeploymentStatusHealthy,
		scaling: verda.ContainerScalingOptions{
			MinReplicaCount:              0,
			MaxReplicaCount:              3,
			ScaleDownPolicy:              &verda.ScalingPolicy{DelaySeconds: 300},
			ScaleUpPolicy:                &verda.ScalingPolicy{DelaySeconds: 0},
			QueueMessageTTLSeconds:       300,
			ConcurrentRequestsPerReplica: 1,
			ScalingTriggers: &verda.ScalingTriggers{
				QueueLoad:      &verda.QueueLoadTrigger{Threshold: 3},
				CPUUtilization: &verda.UtilizationTrigger{Enabled: true, Threshold: 70},
				GPUUtilization: &verda.UtilizationTrigger{},
			},
		},
		replicas: []verda.ReplicaInfo{{
			ID:        "r-1",
			Status:    verda.ReplicaStatusRunning,
			StartedAt: time.Date(2026, 8, 9, 1, 0, 0, 0, time.UTC),
		}},
	}
	return dep
}

// ContainerScaling returns the stored scaling options of a deployment.
func (s *Server) ContainerScaling(name string) verda.ContainerScalingOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.containers[name].scaling
}

// ScalingPatchBodies returns every PATCH .../scaling body as decoded JSON, in
// order — the wire-level view of what a client sent.
func (s *Server) ScalingPatchBodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.scalingPatches)
}

// ContainerActionCount returns how many POST .../{action} calls reached the
// mock for a deployment (action is the path segment, e.g. "purge-queue").
func (s *Server) ContainerActionCount(name, action string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.containerActions[name+"/"+action]
}

// containerLocked returns the fixture or writes a 404.
func (s *Server) containerLocked(w http.ResponseWriter, r *http.Request) (*containerFixture, bool) {
	c, ok := s.containers[r.PathValue("name")]
	if !ok {
		writeError(w, http.StatusNotFound, "deployment not found")
	}
	return c, ok
}

func (s *Server) handleListContainers(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]verda.ContainerDeployment, 0, len(s.containers))
	for _, c := range s.containers {
		out = append(out, c.deployment)
	}
	slices.SortFunc(out, func(a, b verda.ContainerDeployment) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetContainer(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.containerLocked(w, r); ok {
		writeJSON(w, http.StatusOK, c.deployment)
	}
}

func (s *Server) handleContainerStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.containerLocked(w, r); ok {
		writeJSON(w, http.StatusOK, verda.ContainerDeploymentStatus{Status: c.status})
	}
}

func (s *Server) handleGetContainerScaling(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.containerLocked(w, r); ok {
		writeJSON(w, http.StatusOK, c.scaling)
	}
}

// handlePatchContainerScaling merges set fields (an omitted trigger keeps its
// value) and answers 200 with the merged options, like production.
func (s *Server) handlePatchContainerScaling(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var raw map[string]any
	var req verda.UpdateScalingOptionsRequest
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.containerLocked(w, r)
	if !ok {
		return
	}
	s.scalingPatches = append(s.scalingPatches, raw)

	mergeScaling(&c.scaling, &req)
	writeJSON(w, http.StatusOK, c.scaling)
}

func mergeScaling(dst *verda.ContainerScalingOptions, req *verda.UpdateScalingOptionsRequest) {
	if req.MinReplicaCount != nil {
		dst.MinReplicaCount = *req.MinReplicaCount
	}
	if req.MaxReplicaCount != nil {
		dst.MaxReplicaCount = *req.MaxReplicaCount
	}
	if req.ScaleDownPolicy != nil {
		dst.ScaleDownPolicy = req.ScaleDownPolicy
	}
	if req.ScaleUpPolicy != nil {
		dst.ScaleUpPolicy = req.ScaleUpPolicy
	}
	if req.QueueMessageTTLSeconds != nil {
		dst.QueueMessageTTLSeconds = *req.QueueMessageTTLSeconds
	}
	if req.ConcurrentRequestsPerReplica != nil {
		dst.ConcurrentRequestsPerReplica = *req.ConcurrentRequestsPerReplica
	}
	t := req.ScalingTriggers
	if t == nil {
		return
	}
	triggers := verda.ScalingTriggers{}
	if dst.ScalingTriggers != nil {
		triggers = *dst.ScalingTriggers
	}
	if t.QueueLoad != nil {
		triggers.QueueLoad = t.QueueLoad
	}
	if t.CPUUtilization != nil {
		triggers.CPUUtilization = t.CPUUtilization
	}
	if t.GPUUtilization != nil {
		triggers.GPUUtilization = t.GPUUtilization
	}
	dst.ScalingTriggers = &triggers
}

func (s *Server) handleContainerReplicas(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.containerLocked(w, r); ok {
		writeJSON(w, http.StatusOK, verda.DeploymentReplicas{List: c.replicas})
	}
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.containerLocked(w, r)
	if !ok {
		return
	}
	if !slices.Contains([]string{"pause", "resume", "restart", "purge-queue"}, action) {
		writeError(w, http.StatusNotFound, "unknown route: POST "+r.URL.Path)
		return
	}
	s.containerActions[c.deployment.Name+"/"+action]++
	w.WriteHeader(http.StatusNoContent)
}
