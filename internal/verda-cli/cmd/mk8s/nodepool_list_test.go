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
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// poolAPI has the clusters of seededClusters and two pools in "prod", one degraded.
func poolAPI() *fakeAPI {
	api := seededClusters()
	api.addPool("prod", "workers", "active", 3, 3)
	api.addPool("prod", "gpu", "degraded", 2, 1)
	api.pools["prod"][1].Errors = []verda.ManagedKubernetesNodePoolError{
		{Reason: "insufficient_capacity", Message: "no free 1V100.6V", Since: testCreated},
	}
	return api
}

func TestNodePoolList_Table(t *testing.T) {
	t.Parallel()
	api := poolAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "list", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and 2 rows:\n%s", out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "NAME INSTANCE TYPE READY/DESIRED STATE AGE" {
		t.Errorf("header = %q", got)
	}
	if f := strings.Fields(lines[1]); f[0] != "workers" || f[1] != "1V100.6V" || f[2] != "3/3" || f[3] != "active" {
		t.Errorf("workers row = %v", f)
	}
	if f := strings.Fields(lines[2]); f[0] != "gpu" || f[2] != "1/2" || f[3] != "degraded" {
		t.Errorf("gpu row = %v", f)
	}
	want := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/pools"}
	if got := api.paths("GET"); !slices.Equal(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestNodePoolList_LocationSkipsTheClusterLookup(t *testing.T) {
	t.Parallel()
	api := poolAPI()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "np", "ls", "--cluster", "prod", "-l", "FIN-03"); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters/FIN-03/prod/pools"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestNodePoolList_Structured(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"json":  func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"agent": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _, err := runMK8s(t, mk(poolAPI(), t), "node-pool", "list", "--cluster", "prod")
			if err != nil {
				t.Fatal(err)
			}
			got := decodeJSON[[]PoolView](t, out)
			want := []PoolView{
				{Name: "workers", InstanceType: "1V100.6V", Replicas: 3, ReadyReplicas: 3, State: "active", SSHKeyIDs: []string{"key-1"}, Errors: []PoolErrorView{}, CreatedAt: testCreated},
				{
					Name: "gpu", InstanceType: "1V100.6V", Replicas: 2, ReadyReplicas: 1, State: "degraded", SSHKeyIDs: []string{"key-1"},
					Errors: []PoolErrorView{{Reason: "insufficient_capacity", Message: "no free 1V100.6V", Since: testCreated}}, CreatedAt: testCreated,
				},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got  %+v\nwant %+v", got, want)
			}
		})
	}

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()
		out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, poolAPI())), "node-pool", "list", "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[1]["state"] != "degraded" || got[0]["errors"] == nil {
			t.Errorf("got %v", got)
		}
	})
}

func TestNodePoolList_Empty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	out, errOut, err := runMK8s(t, tableFactory(client, nil), "node-pool", "list", "--cluster", "prod")
	if err != nil || !strings.Contains(out, "No node pools in cluster prod.") || !strings.Contains(errOut, "node-pool create --cluster prod") {
		t.Errorf("out=%q err=%q error=%v", out, errOut, err)
	}
	out, _, err = runMK8s(t, jsonFactory(client), "node-pool", "list", "--cluster", "prod")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("empty JSON = %q, error=%v", out, err)
	}
}

func TestNodePoolList_ClusterErrors(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	_, _, err := runMK8s(t, tableFactory(client, nil), "node-pool", "list")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster"}) {
		t.Errorf("missing = %v", missing)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "node-pool", "list", "--cluster", "ghost"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "node-pool", "list", "--cluster", " "); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("blank cluster: %v", err)
	}
}

func TestNodePoolList_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, poolAPI()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "node-pool", "list", "--cluster", "prod")
	if err != nil || !strings.Contains(errOut, "API response: 2 node pool(s):") || !strings.Contains(errOut, `"ready_replicas": 1`) {
		t.Errorf("err=%v debug=%q", err, errOut)
	}
}

func TestNodePoolList_ClusterPicker(t *testing.T) {
	withTerminal(t)
	api := poolAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0)), "node-pool", "list")
	if err != nil || !strings.Contains(out, "workers") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestNodePoolList_NotEnabled(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	api.disabled = true
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "list", "--cluster", "prod", "-l", "FIN-03")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Fatalf("err = %v", err)
	}
}
