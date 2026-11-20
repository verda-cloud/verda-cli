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

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const kcDeletePath = "/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-aaa"

// A named id is not read first (no GET of the kubeconfig, so no YAML either), and there is no --wait.
func TestKubeconfigDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Deletion accepted.", "not revoked", "still work until they expire.", "Deleting the cluster invalidates them", "does not use system:masters"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout misses %q: %q", want, out)
		}
	}
	if dels := api.requestsMatching("DELETE"); len(dels) != 1 || dels[0].Path != kcDeletePath {
		t.Fatalf("DELETE requests = %v", dels)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters"}) {
		t.Errorf("reads = %v, want only the cluster lookup", got)
	}
}

func TestKubeconfigDelete_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	want := ActionResult{Resource: "kubeconfig", ID: "prod-kc-aaa", Name: "prod-kc-aaa", Location: "FIN-03", Cluster: "prod", Action: "delete", Status: "accepted"}
	if got := decodeJSON[ActionResult](t, out); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestKubeconfigDelete_Interactive(t *testing.T) {
	withTerminal(t)
	api := kcFixtureAPI()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Deletes kubeconfig prod-kc-aaa.", "Copies already downloaded are NOT revoked", "keep working until they expire.", "cannot be undone"} {
		if !strings.Contains(errOut, s) {
			t.Errorf("warning misses %q:\n%s", s, errOut)
		}
	}
	if !strings.Contains(out, "still work until they expire.") || len(api.requestsMatching("DELETE")) != 1 {
		t.Errorf("stdout = %q, DELETEs = %d", out, len(api.requestsMatching("DELETE")))
	}
}

func TestKubeconfigDelete_Errors(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "delete", "--yes")
	if m, _ := cmdutil.ClassifyError(err).Details["missing"].([]string); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" || !slices.Equal(m, []string{"--cluster", "<id>"}) {
		t.Errorf("err = %v, missing = %v", err, m)
	}
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, kcFixtureAPI()), nil), "kubeconfig", "delete", "ghost", "--cluster", "prod", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
}
