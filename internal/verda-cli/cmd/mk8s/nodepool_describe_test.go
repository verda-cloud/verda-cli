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

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

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

	out, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "describe", "workers", "--cluster", "prod")
	if err != nil || strings.Contains(out, "Errors:") {
		t.Errorf("a healthy pool shows no Errors section: err=%v\n%s", err, out)
	}
	if got := api.paths("GET"); !slices.Contains(got, "/mk8sclusters/FIN-03/prod/pools/gpu") {
		t.Errorf("requests = %v", got)
	}
}

func TestNodePoolDescribe_AgentAndAliases(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "pool", "show", "workers", "--cluster", "prod", "-l", "FIN-03")
	if err != nil {
		t.Fatal(err)
	}
	if v := decodeJSON[PoolView](t, out); v.Name != "workers" || v.Errors == nil || len(v.Errors) != 0 {
		t.Errorf("view = %+v", v)
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
	if _, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "node-pool", "describe", "  ", "--cluster", "prod"); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("blank pool: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolAPI()), nil), "node-pool", "describe", "ghost", "--cluster", "prod"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown pool: %v", err)
	}
}

func TestNodePoolDescribe_Pickers(t *testing.T) {
	withTerminal(t)

	t.Run("cluster picker then pool picker", func(t *testing.T) {
		api := poolAPI()
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0, 1)), "node-pool", "describe")
		if err != nil || !strings.Contains(out, "gpu") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("pool picker only", func(t *testing.T) {
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolAPI()), pickerAnswers(0)), "node-pool", "describe", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "workers") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("cancel row and Esc end cleanly", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"cancel row": {Prompter: pickerAnswers(2)},
			"esc":        {Prompter: tuitest.New(), selectErr: contextCanceled()},
		} {
			f := tableFactory(newFakeClient(t, poolAPI()), nil)
			f.PrompterOverride = p
			out, errOut, err := runMK8s(t, f, "node-pool", "describe", "--cluster", "prod")
			if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: out=%q err=%q error=%v", name, out, errOut, err)
			}
		}
	})

	t.Run("no pools", func(t *testing.T) {
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), tuitest.New()), "node-pool", "describe", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "No node pools in cluster prod.") || !strings.Contains(errOut, "node-pool create") {
			t.Errorf("out=%q err=%q error=%v", out, errOut, err)
		}
	})
}
