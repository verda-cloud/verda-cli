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

package mk8s

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func describedAPI() *fakeAPI {
	api := seededClusters()
	api.clusters[0].Conditions = append(api.clusters[0].Conditions, conditionReady())
	api.addPool("prod", "workers", "active", 3, 2)
	return api
}

var describeLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*):\s+(\S.*)$`)

// The name is found with one List, then the cluster and its pools are read.
func TestClusterDescribe_Table(t *testing.T) {
	t.Parallel()
	api := describedAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod", "/mk8sclusters/FIN-03/prod/pools"}
	if got := api.paths("GET"); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if m := describeLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = strings.TrimSpace(m[2])
		}
	}
	for k, v := range map[string]string{
		"Name": "prod", "Location": "FIN-03", "Version": "1.37.0", "Phase": "running",
		"Endpoint": "https://prod.example.test:6443", "DNS suffix": "-", "Network": "pn-1", "Subnet": "sn-1",
		"Pod CIDR": "10.100.0.0/16", "Service CIDR": "10.200.0.0/16", "Created": "2026-11-19 09:30 UTC",
	} {
		if got[k] != v {
			t.Errorf("%s: = %q, want %q", k, got[k], v)
		}
	}
	for _, want := range []string{"Conditions:", "TYPE", "Ready", "Node pools:", "INSTANCE TYPE", "READY/DESIRED", "workers", "2/3", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "AllGood") && strings.Index(line, "AllGood") > strings.Index(line, "all components ready") {
			t.Errorf("condition columns are REASON then MESSAGE: %q", line)
		}
	}
}

func TestClusterDescribe_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, describedAPI())), "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	if got["name"] != "prod" || got["phase"] != "running" || got["pod_cidr"] != "10.100.0.0/16" {
		t.Errorf("cluster fields = %v", got)
	}
	pools, ok := got["node_pools"].([]any)
	if !ok || len(pools) != 1 {
		t.Fatalf("node_pools = %v", got["node_pools"])
	}
	if p := pools[0].(map[string]any); p["name"] != "workers" || p["ready_replicas"] != float64(2) || p["replicas"] != float64(3) {
		t.Errorf("pool = %v", p)
	}
	if _, has := got["node_pools_error"]; has {
		t.Error("node_pools_error must be absent when the pool call worked")
	}
	if conds, _ := got["conditions"].([]any); len(conds) != 1 {
		t.Errorf("conditions = %v", got["conditions"])
	}
}

// A failed pool call still prints the cluster: node_pools is null with
// node_pools_error. No pools is [].
func TestClusterDescribe_NodePools(t *testing.T) {
	t.Parallel()
	failing := func() *fakeAPI {
		api := describedAPI()
		api.failures["GET /mk8sclusters/{loc}/{name}/pools"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"pools unavailable"}`}
		return api
	}
	for name, tc := range map[string]struct {
		api            func() *fakeAPI
		cluster        string
		table, errOut  string
		pools, poolErr string
	}{
		"failed": {failing, "prod", "Node pools: unavailable", "Warning: could not load the node pools of cluster prod", "null", "pools unavailable"},
		"none":   {seededClusters, "staging", "node-pool create --cluster staging", "", "[]", ""},
	} {
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, tc.api()), nil), "cluster", "describe", tc.cluster)
		if err != nil || !strings.Contains(out, tc.cluster) || !strings.Contains(out, tc.table) || !strings.Contains(errOut, tc.errOut) {
			t.Errorf("%s: out=%q stderr=%q err=%v", name, out, errOut, err)
		}
		out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, tc.api())), "cluster", "describe", tc.cluster)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(out, `"node_pools": `+tc.pools) {
			t.Errorf("%s: node_pools is not %s: %s", name, tc.pools, out)
		}
		if msg, _ := decodeJSON[map[string]any](t, out)["node_pools_error"].(string); !strings.Contains(msg, tc.poolErr) || (tc.poolErr == "" && msg != "") {
			t.Errorf("%s: node_pools_error = %q", name, msg)
		}
	}
}
