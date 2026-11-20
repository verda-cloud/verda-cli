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
	"slices"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNodePoolDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := poolAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "delete", "workers", "--cluster", "prod", "--yes")
	if err != nil || !strings.Contains(out, "Deletion accepted. Worker VMs are deleted in the background.") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if dels := api.requestsMatching("DELETE"); len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-03/prod/pools/workers" {
		t.Fatalf("DELETE requests = %v", dels)
	}
	// A named pool is not read first: a pool whose read fails must stay deletable.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters"}) {
		t.Errorf("reads = %v, want only the cluster lookup", got)
	}
}

func TestNodePoolDelete_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "np", "rm", "workers", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	want := map[string]any{"resource": "node pool", "name": "workers", "cluster": "prod", "location": "FIN-03", "action": "delete", "status": "accepted"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("result[%s] = %v, want %v", k, got[k], v)
		}
	}
}

func TestNodePoolDelete_MissingTargets(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "node-pool", "delete", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<pool>"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestNodePoolDelete_UnknownPool(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolAPI()), nil), "node-pool", "delete", "ghost", "--cluster", "prod", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("err = %v", err)
	}
}
