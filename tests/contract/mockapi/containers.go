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
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

type containerFixture struct {
	deployment verda.ContainerDeployment
	status     string
	scaling    verda.ContainerScalingOptions
	replicas   []verda.ReplicaInfo
	env        []verda.ContainerEnvVar // variables of Containers[0]
	// staleReads GETs of scaling still return staleScaling after a PATCH.
	staleReads   int
	staleScaling verda.ContainerScalingOptions
}

func (s *Server) registerContainerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /container-deployments", s.handleListContainers)
	mux.HandleFunc("GET /container-deployments/{name}", s.handleGetContainer)
	mux.HandleFunc("GET /container-deployments/{name}/status", s.handleContainerStatus)
	mux.HandleFunc("GET /container-deployments/{name}/scaling", s.handleGetContainerScaling)
	mux.HandleFunc("PATCH /container-deployments/{name}/scaling", s.handlePatchContainerScaling)
	mux.HandleFunc("GET /container-deployments/{name}/replicas", s.handleContainerReplicas)
	mux.HandleFunc("POST /container-deployments", s.handleCreateContainer)
	mux.HandleFunc("DELETE /container-deployments/{name}", s.handleDeleteContainer)
	mux.HandleFunc("POST /container-deployments/{name}/pause", s.containerAction(verda.ContainerDeploymentStatusPaused))
	mux.HandleFunc("POST /container-deployments/{name}/resume", s.containerAction(verda.ContainerDeploymentStatusHealthy))
	mux.HandleFunc("POST /container-deployments/{name}/restart", s.containerAction(""))
	mux.HandleFunc("POST /container-deployments/{name}/purge-queue", s.containerAction(""))
	mux.HandleFunc("GET /container-deployments/{name}/environment-variables", s.handleGetContainerEnv)
	mux.HandleFunc("POST /container-deployments/{name}/environment-variables", s.handleAddContainerEnv)
	mux.HandleFunc("PATCH /container-deployments/{name}/environment-variables", s.handleUpdateContainerEnv)
	mux.HandleFunc("DELETE /container-deployments/{name}/environment-variables", s.handleDeleteContainerEnv)
}

// SeedContainerDeployment stores a healthy deployment with one running
// replica, the queue and CPU triggers enabled, and the GPU trigger disabled
// (read back as production does: threshold 0).
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

// SetContainerStatus overrides the status a seeded deployment reports.
func (s *Server) SetContainerStatus(name, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[name].status = status
}

// SetScalingReadLag makes the next n GETs of a deployment's scaling after each
// PATCH return the options from before it, as production reads lag writes.
func (s *Server) SetScalingReadLag(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scalingReadLag = n
}

// ScalingPatchBodies returns every PATCH .../scaling body as decoded JSON, in
// order — the wire-level view of what a client sent.
func (s *Server) ScalingPatchBodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.scalingPatches)
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
	c, ok := s.containerLocked(w, r)
	if !ok {
		return
	}
	if c.staleReads > 0 {
		c.staleReads--
		writeJSON(w, http.StatusOK, c.staleScaling)
		return
	}
	writeJSON(w, http.StatusOK, c.scaling)
}

// handlePatchContainerScaling merges set fields (an omitted trigger keeps its
// value) and answers 200 with the merged options, like production. A merged
// min > max is a 400 that changes nothing (CC-1024).
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

	merged := c.scaling
	mergeScaling(&merged, &req)
	if merged.MinReplicaCount > merged.MaxReplicaCount {
		writeError(w, http.StatusBadRequest, "The minimum number of replicas must not be greater than the maximum number of replicas.")
		return
	}
	c.staleScaling, c.staleReads = c.scaling, s.scalingReadLag
	c.scaling = merged
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

// SetContainerEnv replaces the variables of a seeded deployment's container.
func (s *Server) SetContainerEnv(name string, env []verda.ContainerEnvVar) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containers[name].env = slices.Clone(env)
}

// ContainerEnv returns the variables of a deployment's container.
func (s *Server) ContainerEnv(name string) []verda.ContainerEnvVar {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.containers[name]
	if !ok {
		return nil
	}
	return slices.Clone(c.env)
}

// ContainerStatus returns the status a deployment reports ("" when unknown).
func (s *Server) ContainerStatus(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.containers[name]; ok {
		return c.status
	}
	return ""
}

// HasContainer reports whether a deployment exists.
func (s *Server) HasContainer(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.containers[name]
	return ok
}

// ContainerCreateBodies returns every POST /container-deployments body as
// decoded JSON, in order — the wire-level view of what a client sent.
func (s *Server) ContainerCreateBodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.containerBodies)
}

// handleCreateContainer stores the deployment as "initializing" with the
// container named "<deployment>-0", as production does.
func (s *Server) handleCreateContainer(w http.ResponseWriter, r *http.Request) {
	raw, req, ok := decodeBody[verda.CreateDeploymentRequest](w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.containerBodies = append(s.containerBodies, raw)
	if _, exists := s.containers[req.Name]; exists {
		writeError(w, http.StatusConflict, "deployment already exists")
		return
	}
	dep := verda.ContainerDeployment{
		Name:            req.Name,
		EndpointBaseURL: "https://containers.datacrunch.io/" + req.Name,
		CreatedAt:       time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		IsSpot:          req.IsSpot,
		Compute:         &verda.ContainerCompute{Name: req.Compute.Name, Size: req.Compute.Size},
	}
	var env []verda.ContainerEnvVar
	for i := range req.Containers {
		c := &req.Containers[i]
		dep.Containers = append(dep.Containers, verda.DeploymentContainer{
			Name:         req.Name + "-" + strconv.Itoa(i),
			Image:        verda.ContainerImage{Image: c.Image},
			ExposedPort:  c.ExposedPort,
			Healthcheck:  c.Healthcheck,
			Env:          c.Env,
			VolumeMounts: c.VolumeMounts,
		})
		if i == 0 {
			env = slices.Clone(c.Env)
		}
	}
	s.containers[req.Name] = &containerFixture{
		deployment: dep,
		status:     verda.ContainerDeploymentStatusInitializing,
		scaling:    req.Scaling,
		env:        env,
	}
	writeJSON(w, http.StatusOK, dep)
}

func (s *Server) handleDeleteContainer(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteQueries = append(s.deleteQueries, r.URL.RawQuery)
	if _, ok := s.containerLocked(w, r); !ok {
		return
	}
	delete(s.containers, r.PathValue("name"))
	w.WriteHeader(http.StatusNoContent)
}

// containerAction answers 200 and, when newStatus is set, moves the fixture to it.
func (s *Server) containerAction(newStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c, ok := s.containerLocked(w, r)
		if !ok {
			return
		}
		if newStatus != "" {
			c.status = newStatus
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

// envGroup is the grouped GET environment-variables response shape.
type envGroup struct {
	ContainerName string                  `json:"container_name"`
	Env           []verda.ContainerEnvVar `json:"env"`
}

func (s *Server) handleGetContainerEnv(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.containerLocked(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, []envGroup{{ContainerName: c.deployment.Containers[0].Name, Env: c.env}})
}

// envContainerLocked: the env endpoints key on the inner container; the
// deployment name is not_found, as in production.
func (s *Server) envContainerLocked(w http.ResponseWriter, r *http.Request, containerName string) (*containerFixture, bool) {
	c, ok := s.containerLocked(w, r)
	if !ok {
		return nil, false
	}
	if containerName != c.deployment.Containers[0].Name {
		writeError(w, http.StatusNotFound, "A container with the name '"+containerName+"' does not exist in the deployment '"+r.PathValue("name")+"'")
		return nil, false
	}
	return c, true
}

func (s *Server) handleAddContainerEnv(w http.ResponseWriter, r *http.Request) {
	_, req, ok := decodeBody[verda.ContainerEnvVarsRequest](w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.envContainerLocked(w, r, req.ContainerName)
	if !ok {
		return
	}
	for i := range req.Env {
		if envIndex(c.env, req.Env[i].Name) >= 0 {
			writeError(w, http.StatusBadRequest, "environment variable already exists: "+req.Env[i].Name)
			return
		}
	}
	c.env = append(c.env, req.Env...)
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleUpdateContainerEnv(w http.ResponseWriter, r *http.Request) {
	_, req, ok := decodeBody[verda.ContainerEnvVarsRequest](w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.envContainerLocked(w, r, req.ContainerName)
	if !ok {
		return
	}
	for i := range req.Env {
		idx := envIndex(c.env, req.Env[i].Name)
		if idx < 0 {
			writeError(w, http.StatusNotFound, "environment variable not found: "+req.Env[i].Name)
			return
		}
		c.env[idx] = req.Env[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleDeleteContainerEnv(w http.ResponseWriter, r *http.Request) {
	_, req, ok := decodeBody[verda.DeleteContainerEnvVarsRequest](w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.envContainerLocked(w, r, req.ContainerName)
	if !ok {
		return
	}
	c.env = slices.DeleteFunc(c.env, func(v verda.ContainerEnvVar) bool { return slices.Contains(req.Env, v.Name) })
	writeJSON(w, http.StatusOK, map[string]any{})
}

func envIndex(env []verda.ContainerEnvVar, name string) int {
	return slices.IndexFunc(env, func(v verda.ContainerEnvVar) bool { return v.Name == name })
}

// decodeBody reads a JSON body both raw (for wire assertions) and typed; a
// bad body answers 400 and returns ok=false.
func decodeBody[T any](w http.ResponseWriter, r *http.Request) (raw map[string]any, req T, ok bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return nil, req, false
	}
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, req, false
	}
	return raw, req, true
}
