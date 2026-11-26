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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNodePoolDescribe_JSONHasEveryFieldInItsPlace(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, poolAPI())), "node-pool", "describe", "gpu", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	want := PoolView{
		Name: "gpu", InstanceType: "1V100.6V", Replicas: 2, ReadyReplicas: 1, State: "degraded", SSHKeyIDs: []string{"key-1"},
		Errors:    []PoolErrorView{{Reason: "insufficient_capacity", Message: "no free 1V100.6V", Since: testCreated}},
		CreatedAt: testCreated,
	}
	if got := decodeJSON[PoolView](t, out); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

var poolDescribeLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*):\s+(\S.*)$`)

func TestNodePoolDescribe_Table(t *testing.T) {
	t.Parallel()
	api := poolAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "describe", "gpu", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if m := poolDescribeLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = strings.TrimSpace(m[2])
		}
	}
	want := map[string]string{
		"Name": "gpu", "Cluster": "prod", "Location": "FIN-03", "Instance type": "1V100.6V",
		"Workers": "1/2 ready", "State": "degraded", "SSH keys": "key-1", "Created": "2026-11-19 09:30 UTC",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: = %q, want %q", k, got[k], v)
		}
	}
	for _, s := range []string{"Errors:", "REASON", "insufficient_capacity", "no free 1V100.6V"} {
		if !strings.Contains(out, s) {
			t.Errorf("output misses %q:\n%s", s, out)
		}
	}
	if got := api.paths("GET"); !slices.Contains(got, "/mk8sclusters/FIN-03/prod/pools/gpu") {
		t.Errorf("requests = %v", got)
	}
}

func TestNodePoolDescribe_MissingTargets(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "node-pool", "describe")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<pool>"}) {
		t.Errorf("missing = %v", missing)
	}
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolAPI()), nil), "node-pool", "describe", "ghost", "--cluster", "prod"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown pool: %v", err)
	}
}
