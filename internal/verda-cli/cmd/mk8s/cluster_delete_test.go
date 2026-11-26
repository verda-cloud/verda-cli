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
	"strings"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestClusterDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.addPool("prod", "workers", "active", 3, 3)
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "delete", "prod", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Deletion accepted. Node pools and worker VMs are deleted in the background.") {
		t.Errorf("stdout = %q", out)
	}
	dels := api.requestsMatching("DELETE")
	if len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-03/prod" {
		t.Fatalf("DELETE requests = %v", dels)
	}
	// No node pool check, no wait.
	for _, p := range api.paths("GET") {
		if strings.Contains(p, "/pools") {
			t.Errorf("delete must not read node pools: %s", p)
		}
	}
	if len(api.requestsMatching("POST"))+len(api.requestsMatching("PATCH")) != 0 {
		t.Error("delete must not write anything else")
	}
}

func TestClusterDelete_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "rm", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	want := map[string]any{"resource": "cluster", "name": "prod", "location": "FIN-03", "action": "delete", "status": "accepted"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("result[%s] = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"cluster", "id"} {
		if _, has := got[k]; has {
			t.Errorf("%q must be absent for a cluster: %v", k, got)
		}
	}
}

func TestClusterDelete_MissingCluster(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "delete", "--yes")
	if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
}

func TestClusterDelete_Interactive(t *testing.T) {
	withTerminal(t)
	for name, tc := range map[string]struct {
		prompter *tuitest.Prompter
		args     []string
		warning  bool
	}{
		"confirmed": {tuitest.New().AddConfirm(true), nil, true},
		// No prompter answers: a confirm would fail the test.
		"--yes skips the confirmation": {tuitest.New(), []string{"--yes"}, false},
	} {
		api := seededClusters()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tc.prompter), append([]string{"cluster", "delete", "prod"}, tc.args...)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, want := range []string{"node pools", "worker VMs", "kubeconfigs", "Downloaded kubeconfigs stop working", "cannot be undone"} {
			if strings.Contains(errOut, want) != tc.warning {
				t.Errorf("%s: warning and %q: %s", name, want, errOut)
			}
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("%s: DELETEs = %d", name, n)
		}
	}
}
