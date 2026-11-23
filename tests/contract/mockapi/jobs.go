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
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Job deployment statuses as the /job-deployments/{name}/status route reports them.
const (
	JobStatusHealthy = "healthy"
	JobStatusPaused  = "paused"
)

type jobFixture struct {
	job    verda.JobDeployment
	status string
}

func (s *Server) registerJobRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /job-deployments", s.handleListJobs)
	mux.HandleFunc("POST /job-deployments", s.handleCreateJob)
	mux.HandleFunc("GET /job-deployments/{name}", s.handleGetJob)
	mux.HandleFunc("DELETE /job-deployments/{name}", s.handleDeleteJob)
	mux.HandleFunc("GET /job-deployments/{name}/status", s.handleJobStatus)
	mux.HandleFunc("POST /job-deployments/{name}/pause", s.jobAction(JobStatusPaused))
	mux.HandleFunc("POST /job-deployments/{name}/resume", s.jobAction(JobStatusHealthy))
	mux.HandleFunc("POST /job-deployments/{name}/purge-queue", s.jobAction(""))
}

// SeedJobDeployment stores a healthy job deployment with a 30 min deadline.
func (s *Server) SeedJobDeployment(name string) verda.JobDeployment {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := verda.JobDeployment{
		Name:            name,
		EndpointBaseURL: "https://containers.datacrunch.io/" + name,
		CreatedAt:       time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		Compute:         &verda.ContainerCompute{Name: "H100", Size: 1},
		Containers: []verda.DeploymentContainer{{
			Name:        name + "-0",
			Image:       verda.ContainerImage{Image: "docker.io/library/nginx:1.27"},
			ExposedPort: 8080,
		}},
		Scaling: &verda.JobScalingOptions{MaxReplicaCount: 3, QueueMessageTTLSeconds: 300, DeadlineSeconds: 1800},
	}
	s.jobs[name] = &jobFixture{job: job, status: JobStatusHealthy}
	return job
}

// HasJob reports whether a job deployment exists.
func (s *Server) HasJob(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.jobs[name]
	return ok
}

// JobStatus returns the status a job deployment reports ("" when unknown).
func (s *Server) JobStatus(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[name]; ok {
		return j.status
	}
	return ""
}

// JobCreateBodies returns every POST /job-deployments body as decoded JSON, in order.
func (s *Server) JobCreateBodies() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.jobBodies)
}

func (s *Server) jobLocked(w http.ResponseWriter, r *http.Request) (*jobFixture, bool) {
	j, ok := s.jobs[r.PathValue("name")]
	if !ok {
		writeError(w, http.StatusNotFound, "job deployment not found")
	}
	return j, ok
}

func (s *Server) handleListJobs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]verda.JobDeploymentShortInfo, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, verda.JobDeploymentShortInfo{Name: j.job.Name, CreatedAt: j.job.CreatedAt, Compute: j.job.Compute})
	}
	slices.SortFunc(out, func(a, b verda.JobDeploymentShortInfo) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	raw, req, ok := decodeBody[verda.CreateJobDeploymentRequest](w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobBodies = append(s.jobBodies, raw)
	if _, exists := s.jobs[req.Name]; exists {
		writeError(w, http.StatusConflict, "job deployment already exists")
		return
	}
	job := verda.JobDeployment{
		Name:                      req.Name,
		EndpointBaseURL:           "https://containers.datacrunch.io/" + req.Name,
		CreatedAt:                 time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		Compute:                   req.Compute,
		ContainerRegistrySettings: req.ContainerRegistrySettings,
		Scaling:                   req.Scaling,
	}
	for i := range req.Containers {
		c := &req.Containers[i]
		job.Containers = append(job.Containers, verda.DeploymentContainer{
			Name:         req.Name + "-" + strconv.Itoa(i),
			Image:        verda.ContainerImage{Image: c.Image},
			ExposedPort:  c.ExposedPort,
			Env:          c.Env,
			VolumeMounts: c.VolumeMounts,
		})
	}
	s.jobs[req.Name] = &jobFixture{job: job, status: JobStatusHealthy}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobLocked(w, r); ok {
		writeJSON(w, http.StatusOK, j.job)
	}
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteQueries = append(s.deleteQueries, r.URL.RawQuery)
	if _, ok := s.jobLocked(w, r); !ok {
		return
	}
	delete(s.jobs, r.PathValue("name"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobLocked(w, r); ok {
		writeJSON(w, http.StatusOK, verda.JobDeploymentStatus{Status: j.status})
	}
}

func (s *Server) jobAction(newStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		j, ok := s.jobLocked(w, r)
		if !ok {
			return
		}
		if newStatus != "" {
			j.status = newStatus
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}
