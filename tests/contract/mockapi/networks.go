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
	"net/http"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

var networkCreated = time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)

// networkFixture holds one private network with its subnets and their routes.
type networkFixture struct {
	network verda.PrivateNetwork
	subnets []verda.Subnet
	routes  map[string][]verda.Route // by subnet ID
}

func (s *Server) registerNetworkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /private-networks", s.handleListNetworks)
	mux.HandleFunc("POST /private-networks", s.handleCreateNetwork)
	mux.HandleFunc("GET /private-networks/{id}", s.handleGetNetwork)
	mux.HandleFunc("DELETE /private-networks/{id}", s.handleDeleteNetwork)
	mux.HandleFunc("GET /private-networks/{id}/subnets", s.handleListSubnets)
	mux.HandleFunc("POST /private-networks/{id}/subnets", s.handleCreateSubnet)
	mux.HandleFunc("DELETE /private-networks/{id}/subnets/{sid}", s.handleDeleteSubnet)
	mux.HandleFunc("GET /private-networks/{id}/subnets/{sid}/routes", s.handleListRoutes)
}

// SeedNetwork stores a private network fixture and returns it.
func (s *Server) SeedNetwork(name string, isDefault bool) verda.PrivateNetwork {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := verda.PrivateNetwork{ID: "pn_" + s.newIDLocked()[24:], Name: name, Mode: verda.NetworkModeCustom, IsDefault: isDefault, CreatedAt: networkCreated}
	s.networks[n.ID] = &networkFixture{network: n, routes: map[string][]verda.Route{}}
	s.networkOrder = append(s.networkOrder, n.ID)
	return n
}

// SeedSubnet stores a subnet fixture in an existing network and returns it.
func (s *Server) SeedSubnet(networkID, name, location, cidr string) verda.Subnet {
	s.mu.Lock()
	defer s.mu.Unlock()
	fx := s.networks[networkID]
	sub := verda.Subnet{
		ID: "sn_" + s.newIDLocked()[24:], Name: name, Location: verda.Location{Code: location}, CIDR: cidr,
		IsDefault: true, TotalIPCount: 251, CreatedAt: networkCreated,
	}
	fx.subnets = append(fx.subnets, sub)
	return sub
}

// AttachInstance puts a seeded instance on a network and subnet.
func (s *Server) AttachInstance(instanceID, networkID, subnetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := s.instances[instanceID]
	privateIP := "10.0.0.5"
	inst.PrivateNetworkID, inst.SubnetID, inst.PrivateIP = &networkID, &subnetID, &privateIP
}

// HasNetwork reports whether a network with the given ID exists.
func (s *Server) HasNetwork(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.networks[id]
	return ok
}

func (s *Server) handleListNetworks(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]verda.PrivateNetwork, 0, len(s.networkOrder))
	for _, id := range s.networkOrder {
		if fx, ok := s.networks[id]; ok {
			out = append(out, fx.network)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req verda.CreatePrivateNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n := s.SeedNetwork(req.Name, req.IsDefault)
	if req.Mode != "" {
		s.mu.Lock()
		s.networks[n.ID].network.Mode = req.Mode
		n = s.networks[n.ID].network
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) networkLocked(w http.ResponseWriter, id string) *networkFixture {
	fx, ok := s.networks[id]
	if !ok {
		writeError(w, http.StatusNotFound, "private network not found")
	}
	return fx
}

func (s *Server) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fx := s.networkLocked(w, r.PathValue("id")); fx != nil {
		writeJSON(w, http.StatusOK, fx.network)
	}
}

func (s *Server) handleDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fx := s.networkLocked(w, r.PathValue("id")); fx != nil {
		delete(s.networks, fx.network.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fx := s.networkLocked(w, r.PathValue("id")); fx != nil {
		out := append([]verda.Subnet{}, fx.subnets...)
		writeJSON(w, http.StatusOK, out)
	}
}

func (s *Server) handleCreateSubnet(w http.ResponseWriter, r *http.Request) {
	var req verda.CreateSubnetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	_, ok := s.networks[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "private network not found")
		return
	}
	writeJSON(w, http.StatusCreated, s.SeedSubnet(r.PathValue("id"), req.Name, req.LocationCode, req.CIDR))
}

func (s *Server) handleDeleteSubnet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fx := s.networkLocked(w, r.PathValue("id"))
	if fx == nil {
		return
	}
	for i := range fx.subnets {
		if fx.subnets[i].ID == r.PathValue("sid") {
			fx.subnets = append(fx.subnets[:i], fx.subnets[i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "subnet not found")
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fx := s.networkLocked(w, r.PathValue("id")); fx != nil {
		out := append([]verda.Route{}, fx.routes[r.PathValue("sid")]...)
		writeJSON(w, http.StatusOK, out)
	}
}
