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
	"strings"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Serverless compute catalog the mock serves; one resource is out of stock.
const (
	ComputeH100       = "H100"
	ComputeRTX4500Ada = "RTX4500Ada"
	ComputeCPU        = "CPUNode"
)

func defaultComputeResources() []verda.ComputeResource {
	return []verda.ComputeResource{
		{Name: ComputeH100, Size: 1, IsAvailable: true},
		{Name: ComputeH100, Size: 2, IsAvailable: true},
		{Name: ComputeRTX4500Ada, Size: 1, IsAvailable: false},
		{Name: ComputeCPU, Size: 1, IsAvailable: true},
	}
}

func (s *Server) registerServerlessCatalogRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /serverless-compute-resources", s.handleComputeResources)
	mux.HandleFunc("GET /secrets", s.handleListSecrets)
	mux.HandleFunc("DELETE /secrets/{name}", s.deleteNamed(func(name string) bool { return deleteKey(s.secrets, name) }))
	mux.HandleFunc("GET /file-secrets", s.handleListFileSecrets)
	mux.HandleFunc("DELETE /file-secrets/{name}", s.deleteNamed(func(name string) bool { return deleteKey(s.fileSecrets, name) }))
	mux.HandleFunc("GET /container-registry-credentials", s.handleListRegistryCredentials)
	mux.HandleFunc("DELETE /container-registry-credentials/{name}", s.deleteNamed(func(name string) bool { return deleteKey(s.registryCreds, name) }))
}

var fixtureCreatedAt = time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)

// SeedSecret stores an env secret; the mock never stores or serves a value.
func (s *Server) SeedSecret(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[name] = verda.Secret{Name: name, CreatedAt: fixtureCreatedAt, SecretType: "env"}
}

// SeedFileSecret stores a file secret with the given file names.
func (s *Server) SeedFileSecret(name string, fileNames ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileSecrets[name] = verda.FileSecret{Name: name, CreatedAt: fixtureCreatedAt, SecretType: "file", FileNames: fileNames}
}

// SeedRegistryCredentials stores registry credentials by name.
func (s *Server) SeedRegistryCredentials(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registryCreds[name] = verda.RegistryCredentials{Name: name, CreatedAt: fixtureCreatedAt}
}

// HasSecret reports whether an env secret exists.
func (s *Server) HasSecret(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.secrets[name]
	return ok
}

// HasFileSecret reports whether a file secret exists.
func (s *Server) HasFileSecret(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.fileSecrets[name]
	return ok
}

// HasRegistryCredentials reports whether registry credentials exist.
func (s *Server) HasRegistryCredentials(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.registryCreds[name]
	return ok
}

func (s *Server) handleComputeResources(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, s.computes)
}

func (s *Server) handleListSecrets(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, sortedByName(s.secrets, func(v verda.Secret) string { return v.Name }))
}

func (s *Server) handleListFileSecrets(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, sortedByName(s.fileSecrets, func(v verda.FileSecret) string { return v.Name }))
}

func (s *Server) handleListRegistryCredentials(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, http.StatusOK, sortedByName(s.registryCreds, func(v verda.RegistryCredentials) string { return v.Name }))
}

// deleteNamed answers 204 when remove finds the name, 404 otherwise, and
// records the query string (force=true) like the other serverless deletes.
func (s *Server) deleteNamed(remove func(name string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.deleteQueries = append(s.deleteQueries, r.URL.RawQuery)
		if !remove(r.PathValue("name")) {
			writeError(w, http.StatusNotFound, "not found: "+r.PathValue("name"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func deleteKey[V any](m map[string]V, name string) bool {
	if _, ok := m[name]; !ok {
		return false
	}
	delete(m, name)
	return true
}

func sortedByName[V any](m map[string]V, name func(V) string) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b V) int { return strings.Compare(name(a), name(b)) })
	return out
}
