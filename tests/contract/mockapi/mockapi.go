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

// Package mockapi provides an in-process mock of the Verda Cloud API for the
// hermetic contract test suite. State lives entirely inside each Server, so
// parallel tests never observe one another. Wire shapes mirror
// verdacloud-sdk-go request/response types.
package mockapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Instance-type catalog used by the mock. Prices replicate the documented
// TOTAL semantics (temp/docs/c1-ondemand-instance.json): price_per_hour /
// spot_price are the whole-instance totals, never per-unit. The 8-GPU type is
// deliberately priced at exactly 8x the 1-GPU sibling so a CLI-side
// re-multiplication regression (review C1) breaks exact equality.
const (
	TypeCPU            = "CPU.4V.16G"
	TypeGPU1           = "1V100.6V"
	TypeGPU8           = "8V100.48V"
	CPUOnDemandTotal   = 0.0279
	CPUSpotTotal       = 0.0098
	GPU1OnDemandTotal  = 0.5
	GPU1SpotTotal      = 0.2
	GPU8OnDemandTotal  = 8 * GPU1OnDemandTotal
	GPU8SpotTotal      = 8 * GPU1SpotTotal
	defaultOSVolumeGiB = 50
)

type catalogEntry struct {
	typ                      verda.InstanceTypeInfo
	onDemandTotal, spotTotal float64
}

const (
	statusProvisioning = "provisioning"
	statusRunning      = "running"
	statusAttached     = "attached"
	statusDetached     = "detached"
)

// Server is a per-test mock Verda API. Each Server owns its fixture state;
// tests seed and inspect it through the Seed*/Has*/Count helpers.
type Server struct {
	srv *httptest.Server

	mu             sync.Mutex
	instances      map[string]*verda.Instance
	volumes        map[string]*verda.Volume
	sshKeys        map[string]*verda.SSHKey
	failures       map[string]int // exact path -> HTTP status override
	forceFormToken bool
	instanceGets   int // GET /instances/{id} count — status polling signal
	idSeq          int
}

// New starts a mock API server. The caller is responsible for Close.
func New() *Server {
	s := &Server{
		instances: map[string]*verda.Instance{},
		volumes:   map[string]*verda.Volume{},
		sshKeys:   map[string]*verda.SSHKey{},
		failures:  map[string]int{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", s.handleToken)
	mux.HandleFunc("GET /instance-types", s.handleInstanceTypes)
	mux.HandleFunc("GET /instances", s.handleListInstances)
	mux.HandleFunc("POST /instances", s.handleCreateInstance)
	mux.HandleFunc("PUT /instances", s.handleInstanceAction)
	mux.HandleFunc("GET /instances/{id}", s.handleGetInstance)
	mux.HandleFunc("GET /volumes", s.handleListVolumes)
	mux.HandleFunc("POST /volumes", s.handleCreateVolume)
	mux.HandleFunc("GET /volumes/{id}", s.handleGetVolume)
	mux.HandleFunc("DELETE /volumes/{id}", s.handleDeleteVolume)
	mux.HandleFunc("GET /ssh-keys", s.handleListSSHKeys)
	mux.HandleFunc("POST /ssh-keys", s.handleCreateSSHKey)
	mux.HandleFunc("DELETE /ssh-keys/{id}", s.handleDeleteSSHKey)
	mux.HandleFunc("GET /scripts", s.handleListScripts)
	mux.HandleFunc("GET /locations", s.handleListLocations)
	mux.HandleFunc("GET /instance-availability", s.handleAvailability)
	mux.HandleFunc("GET /instance-availability/{type}", s.handleTypeAvailability)
	mux.HandleFunc("GET /balance", s.handleBalance)
	mux.HandleFunc("/", s.handleNotFound)

	s.srv = httptest.NewServer(s.guard(mux))
	return s
}

// Close shuts down the underlying HTTP server.
func (s *Server) Close() { s.srv.Close() }

// URL returns the base URL to pass to the CLI via --base-url.
func (s *Server) URL() string { return s.srv.URL }

// FailRoute makes requests to exact path (e.g. "/instances") fail with the
// given HTTP status and a JSON error body until ClearFailures is called.
func (s *Server) FailRoute(path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[path] = status
}

// ClearFailures removes all route failure overrides.
func (s *Server) ClearFailures() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = map[string]int{}
}

// ForceFormTokenFallback makes the token endpoint reject JSON bodies with
// 400 "grant_type not specified" so the SDK retries form-encoded — the
// review-H1 redaction edge.
func (s *Server) ForceFormTokenFallback(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forceFormToken = on
}

// SeedInstance stores an instance fixture and returns it.
func (s *Server) SeedInstance(hostname, instanceType string, pricePerHour float64) verda.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := s.newInstanceLocked(&verda.CreateInstanceRequest{
		InstanceType: instanceType,
		Hostname:     hostname,
		Image:        "ubuntu-24.04",
		LocationCode: verda.LocationFIN01,
	})
	inst.Status = statusRunning
	inst.PricePerHour = verda.FlexibleFloat(pricePerHour)
	s.instances[inst.ID] = &inst
	return inst
}

// SeedVolume stores a detached volume fixture and returns it.
func (s *Server) SeedVolume(name string, sizeGiB int) verda.Volume {
	s.mu.Lock()
	defer s.mu.Unlock()
	vol := s.newVolumeLocked(name, sizeGiB)
	s.volumes[vol.ID] = &vol
	return vol
}

// SeedSSHKey stores an SSH key fixture and returns it.
func (s *Server) SeedSSHKey(name string) verda.SSHKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := verda.SSHKey{
		ID:          s.newIDLocked(),
		Name:        name,
		PublicKey:   "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMockKeyForContractTestsOnly " + name,
		Fingerprint: "SHA256:mockfingerprint",
		CreatedAt:   time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
	}
	s.sshKeys[key.ID] = &key
	return key
}

// HasVolume reports whether a volume with the given ID exists.
func (s *Server) HasVolume(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.volumes[id]
	return ok
}

// VolumeCount returns the number of volumes currently known to the mock.
func (s *Server) VolumeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.volumes)
}

// InstanceGetCount returns how many GET /instances/{id} reads the mock has
// served — the wire-level signal of --wait status polling.
func (s *Server) InstanceGetCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.instanceGets
}

// guard enforces failure overrides before routing.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		status, fail := s.failures[r.URL.Path]
		s.mu.Unlock()
		if fail {
			writeError(w, status, http.StatusText(status))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- token ---

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	forceForm := s.forceFormToken
	s.mu.Unlock()

	if forceForm && r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		writeError(w, http.StatusBadRequest, "grant_type not specified")
		return
	}
	writeJSON(w, http.StatusOK, verda.TokenResponse{
		AccessToken: "mock-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	})
}

// --- instance types / pricing catalog ---

func catalog() []catalogEntry {
	cpu := verda.InstanceTypeInfo{
		ID:           "it-cpu-4v-16g",
		InstanceType: TypeCPU,
		Name:         TypeCPU,
		CPU:          verda.InstanceCPU{Description: "4 vCPU", NumberOfCores: 4},
		Memory:       verda.InstanceMemory{Description: "16GB", SizeInGigabytes: 16},
		PricePerHour: CPUOnDemandTotal,
		SpotPrice:    CPUSpotTotal,
		Currency:     "usd",
	}
	gpu1 := verda.InstanceTypeInfo{
		ID:           "it-1v100-6v",
		InstanceType: TypeGPU1,
		Name:         TypeGPU1,
		CPU:          verda.InstanceCPU{Description: "6 vCPU", NumberOfCores: 6},
		GPU:          verda.InstanceGPU{Description: "1x V100", NumberOfGPUs: 1},
		Memory:       verda.InstanceMemory{Description: "48GB", SizeInGigabytes: 48},
		PricePerHour: GPU1OnDemandTotal,
		SpotPrice:    GPU1SpotTotal,
		Currency:     "usd",
	}
	gpu8 := verda.InstanceTypeInfo{
		ID:           "it-8v100-48v",
		InstanceType: TypeGPU8,
		Name:         TypeGPU8,
		CPU:          verda.InstanceCPU{Description: "48 vCPU", NumberOfCores: 48},
		GPU:          verda.InstanceGPU{Description: "8x V100", NumberOfGPUs: 8},
		Memory:       verda.InstanceMemory{Description: "384GB", SizeInGigabytes: 384},
		PricePerHour: GPU8OnDemandTotal,
		SpotPrice:    GPU8SpotTotal,
		Currency:     "usd",
	}
	return []catalogEntry{
		{typ: cpu, onDemandTotal: CPUOnDemandTotal, spotTotal: CPUSpotTotal},
		{typ: gpu1, onDemandTotal: GPU1OnDemandTotal, spotTotal: GPU1SpotTotal},
		{typ: gpu8, onDemandTotal: GPU8OnDemandTotal, spotTotal: GPU8SpotTotal},
	}
}

func catalogEntryFor(instanceType string) (catalogEntry, bool) {
	entries := catalog()
	for i := range entries {
		if entries[i].typ.InstanceType == instanceType {
			return entries[i], true
		}
	}
	return catalogEntry{}, false
}

func (s *Server) handleInstanceTypes(w http.ResponseWriter, _ *http.Request) {
	entries := catalog()
	types := make([]verda.InstanceTypeInfo, 0, len(entries))
	for i := range entries {
		types = append(types, entries[i].typ)
	}
	writeJSON(w, http.StatusOK, types)
}

// --- instances ---

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	statusFilter := r.URL.Query().Get("status")
	out := make([]verda.Instance, 0, len(s.instances))
	for _, inst := range s.instances {
		advanceLocked(inst)
		if statusFilter != "" && !strings.EqualFold(inst.Status, statusFilter) {
			continue
		}
		out = append(out, *inst)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instanceGets++
	inst, ok := s.instances[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}
	advanceLocked(inst)
	writeJSON(w, http.StatusOK, inst)
}

// advanceLocked mimics the real API completing provisioning: a created
// instance reports "provisioning" once, then "running" from its first read
// on. Lets polling callers (--wait) converge without sleeps.
func advanceLocked(inst *verda.Instance) {
	if inst.Status == statusProvisioning {
		inst.Status = statusRunning
		if inst.IP == nil {
			inst.IP = ptrOf("203.0.113.10")
		}
	}
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req verda.CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "not valid json")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := catalogEntryFor(req.InstanceType); !ok {
		writeError(w, http.StatusBadRequest, "unknown instance type "+req.InstanceType)
		return
	}
	for i := range req.ExistingVolumes {
		volID := req.ExistingVolumes[i]
		if _, ok := s.volumes[volID]; !ok {
			writeError(w, http.StatusNotFound, "volume "+volID+" not found")
			return
		}
	}

	inst := s.newInstanceLocked(&req)
	s.instances[inst.ID] = &inst
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	var req verda.InstanceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "not valid json")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	results := make([]verda.InstanceActionResult, 0, len(req.ID))
	for i := range req.ID {
		results = append(results, s.applyActionLocked(req, req.ID[i]))
	}
	allOK := true
	for i := range results {
		if results[i].Error != "" {
			allOK = false
		}
	}
	status := http.StatusAccepted
	if !allOK {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, results)
}

// applyActionLocked mutates instance state for one action target. For delete,
// an explicit volume_ids list scopes which volumes die with the instance;
// a nil list follows the API default of deleting the OS volume.
func (s *Server) applyActionLocked(req verda.InstanceActionRequest, id string) verda.InstanceActionResult {
	result := verda.InstanceActionResult{Action: req.Action, InstanceID: id}
	inst, ok := s.instances[id]
	if !ok {
		result.Status = "failed"
		result.Error = "instance not found"
		result.StatusCode = http.StatusNotFound
		return result
	}

	switch req.Action {
	case verda.ActionDelete:
		deleteIDs := req.VolumeIDs
		if deleteIDs == nil && inst.OSVolumeID != nil {
			deleteIDs = []string{*inst.OSVolumeID}
		}
		for i := range deleteIDs {
			delete(s.volumes, deleteIDs[i])
		}
		delete(s.instances, id)
	case verda.ActionShutdown, verda.ActionForceShutdown:
		inst.Status = "offline"
	case verda.ActionBoot, verda.ActionStart:
		inst.Status = statusRunning
	}
	result.Status = "completed"
	return result
}

// newInstanceLocked builds an instance from a create request, allocating its
// OS volume and pricing it at the catalog TOTAL (review C1 semantics).
func (s *Server) newInstanceLocked(req *verda.CreateInstanceRequest) verda.Instance {
	entry, _ := catalogEntryFor(req.InstanceType)

	osVolName := req.Hostname + "-os"
	osVolSize := defaultOSVolumeGiB
	if req.OSVolume != nil {
		if req.OSVolume.Name != "" {
			osVolName = req.OSVolume.Name
		}
		if req.OSVolume.Size > 0 {
			osVolSize = req.OSVolume.Size
		}
	}
	osVol := s.newVolumeLocked(osVolName, osVolSize)
	osVol.IsOSVolume = true
	osVol.Status = statusAttached
	s.volumes[osVol.ID] = &osVol

	volumeIDs := make([]string, 0, len(req.Volumes)+len(req.ExistingVolumes))
	for i := range req.Volumes {
		v := s.newVolumeSizedLocked(&req.Volumes[i], req.Hostname)
		s.volumes[v.ID] = &v
		volumeIDs = append(volumeIDs, v.ID)
	}
	volumeIDs = append(volumeIDs, req.ExistingVolumes...)

	price := entry.onDemandTotal
	if req.IsSpot {
		price = entry.spotTotal
	}

	location := req.LocationCode
	if location == "" {
		location = verda.LocationFIN01
	}
	description := req.Description
	if description == "" {
		description = req.Hostname
	}
	contract := req.Contract
	if contract == "" {
		contract = "PAY_AS_YOU_GO"
		if req.IsSpot {
			contract = "SPOT"
		}
	}

	inst := verda.Instance{
		ID:           s.newIDLocked(),
		Status:       statusProvisioning,
		CreatedAt:    time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		CPU:          entry.typ.CPU,
		GPU:          entry.typ.GPU,
		Memory:       entry.typ.Memory,
		Hostname:     req.Hostname,
		Description:  description,
		Location:     location,
		PricePerHour: verda.FlexibleFloat(price),
		IsSpot:       req.IsSpot,
		InstanceType: req.InstanceType,
		Image:        req.Image,
		OSName:       req.Image,
		SSHKeyIDs:    append([]string{}, req.SSHKeyIDs...),
		OSVolumeID:   ptrOf(osVol.ID),
		VolumeIDs:    volumeIDs,
		Contract:     contract,
	}
	if req.StartupScriptID != nil {
		inst.StartupScriptID = req.StartupScriptID
	}
	return inst
}

// --- volumes ---

func (s *Server) handleListVolumes(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]verda.Volume, 0, len(s.volumes))
	for _, vol := range s.volumes {
		out = append(out, *vol)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetVolume(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vol, ok := s.volumes[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "volume not found")
		return
	}
	writeJSON(w, http.StatusOK, vol)
}

func (s *Server) handleCreateVolume(w http.ResponseWriter, r *http.Request) {
	var req verda.VolumeCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "not valid json")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	vol := s.newVolumeLocked(req.Name, req.Size)
	vol.Type = req.Type
	s.volumes[vol.ID] = &vol
	// The API answers volume creates with the bare ID, not JSON.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(vol.ID))
}

func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	if _, ok := s.volumes[id]; !ok {
		writeError(w, http.StatusNotFound, "volume not found")
		return
	}
	delete(s.volumes, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newVolumeLocked(name string, sizeGiB int) verda.Volume {
	return verda.Volume{
		ID:        s.newIDLocked(),
		Name:      name,
		Size:      sizeGiB,
		Type:      verda.VolumeTypeNVMe,
		Status:    statusDetached,
		CreatedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
		Location:  verda.LocationFIN01,
		SSHKeyIDs: []string{},
		Currency:  "usd",
		Contract:  "PAY_AS_YOU_GO",
	}
}

func (s *Server) newVolumeSizedLocked(req *verda.VolumeCreateRequest, hostname string) verda.Volume {
	name := req.Name
	if name == "" {
		name = hostname + "-storage"
	}
	vol := s.newVolumeLocked(name, req.Size)
	if req.Type != "" {
		vol.Type = req.Type
	}
	return vol
}

// --- ssh keys ---

func (s *Server) handleListSSHKeys(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]verda.SSHKey, 0, len(s.sshKeys))
	for _, key := range s.sshKeys {
		out = append(out, *key)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	var req verda.CreateSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "not valid json")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key := verda.SSHKey{
		ID:        s.newIDLocked(),
		Name:      req.Name,
		PublicKey: req.PublicKey,
		CreatedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}
	s.sshKeys[key.ID] = &key
	writeJSON(w, http.StatusOK, key)
}

func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := r.PathValue("id")
	if _, ok := s.sshKeys[id]; !ok {
		writeError(w, http.StatusNotFound, "ssh key not found")
		return
	}
	delete(s.sshKeys, id)
	w.WriteHeader(http.StatusNoContent)
}

// --- misc read-only sets ---

func (s *Server) handleListScripts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []verda.StartupScript{})
}

func (s *Server) handleListLocations(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []verda.Location{
		{Code: verda.LocationFIN01, Name: "Helsinki 1", CountryCode: "FI"},
		{Code: verda.LocationFIN03, Name: "Helsinki 3", CountryCode: "FI"},
	})
}

func (s *Server) handleAvailability(w http.ResponseWriter, _ *http.Request) {
	entries := catalog()
	types := make([]string, 0, len(entries))
	for i := range entries {
		types = append(types, entries[i].typ.InstanceType)
	}
	writeJSON(w, http.StatusOK, []verda.LocationAvailability{
		{LocationCode: verda.LocationFIN01, Availabilities: types},
		{LocationCode: verda.LocationFIN03, Availabilities: types},
	})
}

func (s *Server) handleTypeAvailability(w http.ResponseWriter, _ *http.Request) {
	// The real API answers the JSON string "true"/"false", not a boolean.
	writeJSON(w, http.StatusOK, "true")
}

func (s *Server) handleBalance(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, verda.Balance{Amount: 500, Currency: "usd"})
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "unknown route: "+r.Method+" "+r.URL.Path)
}

// --- helpers ---

func (s *Server) newIDLocked() string {
	s.idSeq++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.idSeq)
}

func ptrOf(v string) *string { return &v }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, verda.APIError{
		StatusCode: status,
		Code:       strings.ToUpper(strings.ReplaceAll(http.StatusText(status), " ", "_")),
		Message:    message,
	})
}
