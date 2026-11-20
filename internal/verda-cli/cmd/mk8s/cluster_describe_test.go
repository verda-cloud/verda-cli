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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func describedAPI() *fakeAPI {
	api := seededClusters()
	api.clusters[0].Conditions = append(api.clusters[0].Conditions, conditionReady())
	api.addPool("prod", "workers", "active", 3, 2)
	return api
}

func TestClusterDescribe_Table(t *testing.T) {
	t.Parallel()
	api := describedAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Name:", "prod", "Location:", "FIN-03", "Version:", "1.37.0", "Phase:", "running",
		"https://prod.example.test:6443", "pn-1", "sn-1", "10.100.0.0/16", "10.200.0.0/16",
		"Conditions:", "TYPE", "Ready", "Node pools:", "INSTANCE TYPE", "READY/DESIRED", "workers", "2/3", "active",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}

func TestClusterDescribe_OneListThenGetThenPools(t *testing.T) {
	t.Parallel()
	api := describedAPI()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod", "/mk8sclusters/FIN-03/prod/pools"}
	if got := api.paths("GET"); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestClusterDescribe_Structured(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]func(t *testing.T, api *fakeAPI) (string, error){
		"json": func(t *testing.T, api *fakeAPI) (string, error) {
			out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), "cluster", "describe", "prod")
			return out, err
		},
		"agent": func(t *testing.T, api *fakeAPI) (string, error) {
			out, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "cluster", "describe", "prod")
			return out, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := f(t, describedAPI())
			if err != nil {
				t.Fatal(err)
			}
			got := decodeJSON[map[string]any](t, out)
			if got["name"] != "prod" || got["phase"] != "running" {
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
		})
	}
}

func TestClusterDescribe_PoolCallFailsStillPrintsCluster(t *testing.T) {
	t.Parallel()
	newAPI := func() *fakeAPI {
		api := describedAPI()
		api.failures["GET /mk8sclusters/{loc}/{name}/pools"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"pools unavailable"}`}
		return api
	}

	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, newAPI()), nil), "cluster", "describe", "prod", "-l", "FIN-03")
	if err != nil {
		t.Fatalf("the cluster must still print: %v", err)
	}
	if !strings.Contains(out, "prod") || !strings.Contains(out, "Node pools: unavailable") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "Warning: could not load the node pools of cluster prod") {
		t.Errorf("stderr = %q", errOut)
	}

	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, newAPI())), "cluster", "describe", "prod", "-l", "FIN-03")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	if v, present := got["node_pools"]; !present || v != nil {
		t.Errorf("node_pools = %v, want null", v)
	}
	if msg, _ := got["node_pools_error"].(string); !strings.Contains(msg, "pools unavailable") {
		t.Errorf("node_pools_error = %q", msg)
	}
}

func TestClusterDescribe_NoPools(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Node pools: none") || !strings.Contains(out, "node-pool create --cluster staging") {
		t.Errorf("output = %s", out)
	}
	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, api)), "cluster", "describe", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if pools, ok := decodeJSON[map[string]any](t, out)["node_pools"].([]any); !ok || len(pools) != 0 {
		t.Errorf("no pools must be [], got %v", pools)
	}
}

func TestClusterDescribe_Aliases(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	for _, alias := range []string{"get", "show"} {
		if _, _, err := runMK8s(t, tableFactory(client, nil), "cluster", alias, "prod"); err != nil {
			t.Errorf("alias %s: %v", alias, err)
		}
	}
}

// wantDescribed is describedAPI's "prod" cluster. Every field has its own value, so a
// swapped or dropped field fails.
func wantDescribed() ClusterDescribeView {
	return ClusterDescribeView{
		ClusterView: ClusterView{
			Name: "prod", Location: "FIN-03", KubernetesVersion: "1.37.0", Phase: "running",
			ExternalEndpoint: "https://prod.example.test:6443", NetworkID: "pn-1", SubnetID: "sn-1",
			PodCIDR: "10.100.0.0/16", ServiceCIDR: "10.200.0.0/16", DNSSuffix: "",
			Conditions: []ConditionView{{Type: "Ready", Status: "True", Reason: "AllGood", Message: "all components ready", LastTransitionTime: testCreated}},
			CreatedAt:  testCreated,
		},
		NodePools: &[]PoolSummaryView{{Name: "workers", InstanceType: "1V100.6V", ReadyReplicas: 2, Replicas: 3, State: "active"}},
	}
}

func TestClusterDescribe_JSONHasEveryFieldInItsPlace(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, describedAPI())), "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[ClusterDescribeView](t, out)
	if want := wantDescribed(); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

var describeLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*):\s+(\S.*)$`)

func TestClusterDescribe_TableLabelsMatchValues(t *testing.T) {
	t.Parallel()
	api := describedAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if m := describeLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = strings.TrimSpace(m[2])
		}
	}
	want := map[string]string{
		"Name": "prod", "Location": "FIN-03", "Version": "1.37.0", "Phase": "running",
		"Endpoint": "https://prod.example.test:6443", "DNS suffix": "-", "Network": "pn-1", "Subnet": "sn-1",
		"Pod CIDR": "10.100.0.0/16", "Service CIDR": "10.200.0.0/16", "Created": "2026-11-19 09:30 UTC",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: = %q, want %q", k, got[k], v)
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "Ready") && strings.Contains(line, "AllGood") {
			if strings.Index(line, "AllGood") > strings.Index(line, "all components ready") {
				t.Errorf("condition columns are REASON then MESSAGE: %q", line)
			}
		}
	}
}

func TestClusterDescribe_YAMLNullVersusEmptyPools(t *testing.T) {
	t.Parallel()
	read := func(api *fakeAPI) map[string]any {
		out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, api)), "cluster", "describe", "prod", "-l", "FIN-03")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := yaml.Unmarshal([]byte(out), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	failing := describedAPI()
	failing.failures["GET /mk8sclusters/{loc}/{name}/pools"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"down"}`}
	got := read(failing)
	if v, present := got["node_pools"]; !present || v != nil {
		t.Errorf("a failed pool read must be null in YAML too, got %v", v)
	}
	if got["node_pools_error"] == nil || got["name"] != "prod" || got["pod_cidr"] != "10.100.0.0/16" {
		t.Errorf("YAML = %v", got)
	}

	empty := seededClusters()
	empty.clusters[0].Name = "prod"
	if pools, ok := read(empty)["node_pools"].([]any); !ok || len(pools) != 0 {
		t.Errorf("no pools must be [], got %v", read(empty)["node_pools"])
	}

	if pools, ok := read(describedAPI())["node_pools"].([]any); !ok || len(pools) != 1 {
		t.Errorf("pools = %v", pools)
	}
}

func TestClusterDescribe_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, describedAPI()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "cluster", "describe", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "API response: cluster:") || !strings.Contains(errOut, `"pod_cidr": "10.100.0.0/16"`) {
		t.Errorf("debug output = %q", errOut)
	}
}
