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

package util

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func networkFakeClient(t *testing.T) *verda.Client {
	t.Helper()
	networks := []verda.PrivateNetwork{
		{ID: "pn_a", Name: "alpha", IsDefault: true},
		{ID: "pn_b", Name: "beta"},
	}
	subnets := map[string][]verda.Subnet{
		"pn_a": {{ID: "sn_a1", Name: "web"}, {ID: "sn_a2", Name: "shared"}},
		"pn_b": {{ID: "sn_b1", Name: "db"}, {ID: "sn_b2", Name: "shared"}},
	}
	pa, sa1, pb := "pn_a", "sn_a1", "pn_b"
	instances := []verda.Instance{
		{ID: "i-1", Hostname: "on-a1", PrivateNetworkID: &pa, SubnetID: &sa1},
		{ID: "i-2", Hostname: "on-b", PrivateNetworkID: &pb},
		{ID: "i-3", Hostname: "classic"},
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /private-networks", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, networks) })
	mux.HandleFunc("GET /private-networks/{id}/subnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, subnets[r.PathValue("id")])
	})
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, instances) })
	return newPollTestClient(t, mux)
}

func TestResolveNetwork(t *testing.T) {
	t.Parallel()
	client := networkFakeClient(t)
	ctx := context.Background()

	for in, want := range map[string]string{"pn_b": "pn_b", "alpha": "pn_a"} {
		n, err := ResolveNetwork(ctx, client, in)
		if err != nil || n.ID != want {
			t.Errorf("ResolveNetwork(%q) = %v, %v; want %s", in, n, err, want)
		}
	}
	_, err := ResolveNetwork(ctx, client, "missing")
	if ae := ClassifyError(err); ae.Code != "NOT_FOUND" || ae.ExitCode != ExitNotFound {
		t.Errorf("missing network: got %+v", ae)
	}
}

func TestResolveSubnet(t *testing.T) {
	t.Parallel()
	client := networkFakeClient(t)
	ctx := context.Background()

	tests := []struct {
		name, network, subnet string
		wantNet, wantSub      string
		wantCode              string
	}{
		{name: "by ID with network", network: "alpha", subnet: "sn_a1", wantNet: "pn_a", wantSub: "sn_a1"},
		{name: "by name with network", network: "pn_b", subnet: "db", wantNet: "pn_b", wantSub: "sn_b1"},
		{name: "ID without network searches all", subnet: "sn_b2", wantNet: "pn_b", wantSub: "sn_b2"},
		{name: "unique name without network", subnet: "web", wantNet: "pn_a", wantSub: "sn_a1"},
		{name: "repeated name with network", network: "beta", subnet: "shared", wantNet: "pn_b", wantSub: "sn_b2"},
		{name: "ambiguous name", subnet: "shared", wantCode: "VALIDATION_ERROR"},
		{name: "not found", subnet: "nope", wantCode: "NOT_FOUND"},
		{name: "not in that network", network: "alpha", subnet: "db", wantCode: "NOT_FOUND"},
		{name: "network not found", network: "gamma", subnet: "db", wantCode: "NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, s, err := ResolveSubnet(ctx, client, tt.network, tt.subnet)
			if tt.wantCode != "" {
				if ae := ClassifyError(err); ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("err = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n.ID != tt.wantNet || s.ID != tt.wantSub {
				t.Errorf("got %s/%s, want %s/%s", n.ID, s.ID, tt.wantNet, tt.wantSub)
			}
		})
	}
}

func TestListInstancesOn(t *testing.T) {
	t.Parallel()
	client := networkFakeClient(t)
	ctx := context.Background()

	tests := []struct {
		network, subnet string
		want            []string
	}{
		{network: "pn_a", want: []string{"i-1"}},
		{network: "pn_a", subnet: "sn_a1", want: []string{"i-1"}},
		{network: "pn_a", subnet: "sn_a2"},
		{network: "pn_b", want: []string{"i-2"}},
		{network: "pn_b", subnet: "sn_b1"},
	}
	for _, tt := range tests {
		got, err := ListInstancesOn(ctx, client, tt.network, tt.subnet)
		if err != nil {
			t.Fatalf("ListInstancesOn: %v", err)
		}
		ids := make([]string, len(got))
		for i := range got {
			ids[i] = got[i].ID
		}
		if len(ids) != len(tt.want) || (len(ids) > 0 && ids[0] != tt.want[0]) {
			t.Errorf("%s/%s: got %v, want %v", tt.network, tt.subnet, ids, tt.want)
		}
	}
}
