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

package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var testCreated = time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)

type recordedRequest struct {
	Method string
	Path   string
	Body   string
}

type injectedError struct {
	status int
	body   string
}

// fakeAPI is an in-memory private-network API. Subnets and routes are keyed by parent ID.
type fakeAPI struct {
	mu        sync.Mutex
	networks  []verda.PrivateNetwork
	subnets   map[string][]verda.Subnet
	routes    map[string][]verda.Route
	instances []verda.Instance
	locations []verda.Location
	errors    map[string]injectedError
	requests  []recordedRequest
	nextID    int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		subnets: map[string][]verda.Subnet{},
		routes:  map[string][]verda.Route{},
		errors:  map[string]injectedError{},
		locations: []verda.Location{
			{Code: "FIN-01", Name: "Finland 1"},
			{Code: "FIN-03", Name: "Finland 3", IsPrivateNetworksEnabled: true},
			{Code: "US-SOUTHWEST-1", Name: "US Southwest 1", IsPrivateNetworksEnabled: true},
		},
	}
}

func (a *fakeAPI) addNetwork(id, name string, isDefault bool) {
	a.networks = append(a.networks, verda.PrivateNetwork{ID: id, Name: name, Mode: verda.NetworkModeCustom, IsDefault: isDefault, CreatedAt: testCreated})
}

func (a *fakeAPI) addSubnet(networkID, id, name, location, cidr string, isDefault bool) {
	a.subnets[networkID] = append(a.subnets[networkID], verda.Subnet{
		ID: id, Name: name, Location: verda.Location{Code: location}, CIDR: cidr,
		IsDefault: isDefault, UsedIPCount: 1, TotalIPCount: 251, CreatedAt: testCreated,
	})
}

func (a *fakeAPI) addInstance(id, hostname, location, networkID, subnetID, publicIP string) {
	inst := verda.Instance{ID: id, Hostname: hostname, Location: location, Status: "running"}
	privateIP := "10.0.0.5"
	inst.PrivateIP = &privateIP
	if networkID != "" {
		inst.PrivateNetworkID = &networkID
	}
	if subnetID != "" {
		inst.SubnetID = &subnetID
	}
	if publicIP != "" {
		inst.IP = &publicIP
	}
	a.instances = append(a.instances, inst)
}

// failOn makes "METHOD /path" answer with status and an API error body.
func (a *fakeAPI) failOn(methodPath string, status int, code, message string) {
	body, _ := json.Marshal(map[string]string{"code": code, "message": message})
	a.errors[methodPath] = injectedError{status: status, body: string(body)}
}

func (a *fakeAPI) requestsMatching(method string) []recordedRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []recordedRequest
	for _, r := range a.requests {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (a *fakeAPI) id(prefix string) string {
	a.nextID++
	return fmt.Sprintf("%s_new%d", prefix, a.nextID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, what string) {
	writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": what + " not found"})
}

func (a *fakeAPI) findNetwork(id string) int {
	for i := range a.networks {
		if a.networks[i].ID == id {
			return i
		}
	}
	return -1
}

func (a *fakeAPI) findSubnet(networkID, id string) int {
	for i := range a.subnets[networkID] {
		if a.subnets[networkID][i].ID == id {
			return i
		}
	}
	return -1
}

func (a *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /locations", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, a.locations) })
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, a.instances) })
	mux.HandleFunc("GET /private-networks", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, a.networks) })
	mux.HandleFunc("POST /private-networks", a.createNetwork)
	mux.HandleFunc("GET /private-networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if i := a.findNetwork(r.PathValue("id")); i >= 0 {
			writeJSON(w, http.StatusOK, a.networks[i])
			return
		}
		notFound(w, "private network")
	})
	mux.HandleFunc("PATCH /private-networks/{id}", a.updateNetwork)
	mux.HandleFunc("DELETE /private-networks/{id}", func(w http.ResponseWriter, r *http.Request) {
		i := a.findNetwork(r.PathValue("id"))
		if i < 0 {
			notFound(w, "private network")
			return
		}
		a.networks = append(a.networks[:i], a.networks[i+1:]...)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /private-networks/{id}/subnets", func(w http.ResponseWriter, r *http.Request) {
		if a.findNetwork(r.PathValue("id")) < 0 {
			notFound(w, "private network")
			return
		}
		subnets := a.subnets[r.PathValue("id")]
		if subnets == nil {
			subnets = []verda.Subnet{}
		}
		writeJSON(w, http.StatusOK, subnets)
	})
	mux.HandleFunc("POST /private-networks/{id}/subnets", a.createSubnet)
	mux.HandleFunc("GET /private-networks/{id}/subnets/{sid}", func(w http.ResponseWriter, r *http.Request) {
		if i := a.findSubnet(r.PathValue("id"), r.PathValue("sid")); i >= 0 {
			writeJSON(w, http.StatusOK, a.subnets[r.PathValue("id")][i])
			return
		}
		notFound(w, "subnet")
	})
	mux.HandleFunc("PATCH /private-networks/{id}/subnets/{sid}", a.updateSubnet)
	mux.HandleFunc("DELETE /private-networks/{id}/subnets/{sid}", func(w http.ResponseWriter, r *http.Request) {
		nid := r.PathValue("id")
		i := a.findSubnet(nid, r.PathValue("sid"))
		if i < 0 {
			notFound(w, "subnet")
			return
		}
		a.subnets[nid] = append(a.subnets[nid][:i], a.subnets[nid][i+1:]...)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /private-networks/{id}/subnets/{sid}/routes", func(w http.ResponseWriter, r *http.Request) {
		routes := a.routes[r.PathValue("sid")]
		if routes == nil {
			routes = []verda.Route{}
		}
		writeJSON(w, http.StatusOK, routes)
	})
	mux.HandleFunc("POST /private-networks/{id}/subnets/{sid}/routes", a.createRoute)
	mux.HandleFunc("DELETE /private-networks/{id}/subnets/{sid}/routes/{rid}", func(w http.ResponseWriter, r *http.Request) {
		sid := r.PathValue("sid")
		for i := range a.routes[sid] {
			if a.routes[sid][i].ID == r.PathValue("rid") {
				a.routes[sid] = append(a.routes[sid][:i], a.routes[sid][i+1:]...)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		notFound(w, "route")
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.URL.Path != "/oauth2/token" {
			a.requests = append(a.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		}
		if e, ok := a.errors[r.Method+" "+r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(e.status)
			_, _ = io.WriteString(w, e.body)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *fakeAPI) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req verda.CreatePrivateNetworkRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	mode := req.Mode
	if mode == "" {
		mode = verda.NetworkModeCustom
	}
	if req.IsDefault {
		for i := range a.networks {
			a.networks[i].IsDefault = false
		}
	}
	n := verda.PrivateNetwork{ID: a.id("pn"), Name: req.Name, Mode: mode, IsDefault: req.IsDefault, CreatedAt: testCreated}
	a.networks = append(a.networks, n)
	writeJSON(w, http.StatusCreated, n)
}

func (a *fakeAPI) updateNetwork(w http.ResponseWriter, r *http.Request) {
	i := a.findNetwork(r.PathValue("id"))
	if i < 0 {
		notFound(w, "private network")
		return
	}
	var req verda.UpdatePrivateNetworkRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Name != nil {
		a.networks[i].Name = *req.Name
	}
	if req.IsDefault != nil && *req.IsDefault {
		for j := range a.networks {
			a.networks[j].IsDefault = j == i
		}
	}
	writeJSON(w, http.StatusOK, a.networks[i])
}

func (a *fakeAPI) createSubnet(w http.ResponseWriter, r *http.Request) {
	nid := r.PathValue("id")
	if a.findNetwork(nid) < 0 {
		notFound(w, "private network")
		return
	}
	var req verda.CreateSubnetRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	first := true
	for _, s := range a.subnets[nid] {
		if s.Location.Code == req.LocationCode {
			first = false
		}
	}
	s := verda.Subnet{
		ID: a.id("sn"), Name: req.Name, Location: verda.Location{Code: req.LocationCode}, CIDR: req.CIDR,
		IsDefault: req.IsDefault || first, TotalIPCount: 251, CreatedAt: testCreated,
	}
	a.subnets[nid] = append(a.subnets[nid], s)
	writeJSON(w, http.StatusCreated, s)
}

func (a *fakeAPI) updateSubnet(w http.ResponseWriter, r *http.Request) {
	nid := r.PathValue("id")
	i := a.findSubnet(nid, r.PathValue("sid"))
	if i < 0 {
		notFound(w, "subnet")
		return
	}
	var req verda.UpdateSubnetRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	subnets := a.subnets[nid]
	if req.Name != nil {
		subnets[i].Name = *req.Name
	}
	if req.IsDefault != nil && *req.IsDefault {
		for j := range subnets {
			if subnets[j].Location.Code == subnets[i].Location.Code {
				subnets[j].IsDefault = j == i
			}
		}
	}
	writeJSON(w, http.StatusOK, subnets[i])
}

func (a *fakeAPI) createRoute(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	var req verda.CreateRouteRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	gw := &verda.AttachedInstance{ID: req.InstanceID}
	for i := range a.instances {
		if a.instances[i].ID == req.InstanceID {
			gw.Hostname = a.instances[i].Hostname
			gw.PublicIP = a.instances[i].IP
		}
	}
	for i := range a.routes[sid] {
		if a.routes[sid][i].Destination == req.Destination {
			a.routes[sid][i].Instance = gw
			writeJSON(w, http.StatusCreated, a.routes[sid][i])
			return
		}
	}
	rt := verda.Route{ID: a.id("rt"), SubnetID: sid, Destination: req.Destination, Instance: gw, CreatedAt: testCreated}
	a.routes[sid] = append(a.routes[sid], rt)
	writeJSON(w, http.StatusCreated, rt)
}

// newFakeClient serves api and returns an SDK client pointed at it.
func newFakeClient(t *testing.T, api *fakeAPI) *verda.Client {
	t.Helper()
	srv := httptest.NewServer(api.handler())
	t.Cleanup(srv.Close)
	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	return client
}

// Factory shapes for the three modes every command is tested in.
func tableFactory(client *verda.Client, p *tuitest.Prompter) *cmdutil.TestFactory {
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}
	if p != nil {
		f.PrompterOverride = p
	}
	return f
}

func agentFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, AgentModeOverride: true, OutputFormatOverride: "json"}
}

func jsonFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: "json"}
}

// runNetwork runs `verda network <args>` and returns stdout and stderr.
func runNetwork(t *testing.T, f cmdutil.Factory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewCmdNetwork(f, cmdutil.IOStreams{Out: &out, ErrOut: &errOut})
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// withTerminal makes interactiveTTY see a terminal; not parallel-safe.
func withTerminal(t *testing.T) {
	t.Helper()
	orig := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = orig })
}

func agentErrorCode(err error) string {
	var ae *cmdutil.AgentError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}
