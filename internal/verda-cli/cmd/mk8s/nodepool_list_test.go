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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

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

func TestNodePoolList_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, poolAPI())), "node-pool", "list", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	want := []PoolView{
		{Name: "workers", InstanceType: "1V100.6V", Replicas: 3, ReadyReplicas: 3, State: "active", SSHKeyIDs: []string{"key-1"}, Errors: []PoolErrorView{}, CreatedAt: testCreated},
		{
			Name: "gpu", InstanceType: "1V100.6V", Replicas: 2, ReadyReplicas: 1, State: "degraded", SSHKeyIDs: []string{"key-1"},
			Errors: []PoolErrorView{{Reason: "insufficient_capacity", Message: "no free 1V100.6V", Since: testCreated}}, CreatedAt: testCreated,
		},
	}
	if got := decodeJSON[[]PoolView](t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
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

func TestNodePoolList_MissingCluster(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "node-pool", "list")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster"}) {
		t.Errorf("missing = %v", missing)
	}
}
