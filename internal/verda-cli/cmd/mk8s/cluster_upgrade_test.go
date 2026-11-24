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

func TestClusterUpgrade_YesSendsTheNewVersion(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil),
		"cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Upgrade of cluster prod from 1.37.0 to 1.38.0 accepted (FIN-03).") {
		t.Errorf("stdout = %q", out)
	}
	for _, want := range []string{"verda mk8s cluster describe prod -l FIN-03", "`describe` may show `degraded` while the rollout runs."} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr misses %q: %q", want, errOut)
		}
	}

	patches := api.requestsMatching("PATCH")
	if len(patches) != 1 || patches[0].Path != "/mk8sclusters/FIN-03/prod" {
		t.Fatalf("PATCH requests = %v", patches)
	}
	if body := decodeJSON[map[string]string](t, patches[0].Body); len(body) != 1 || body["kubernetes_version"] != "1.38.0" {
		t.Errorf("body = %v", body)
	}
	// Name lookup, then the read for the "from" version, then the update. No wait.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod"}) {
		t.Errorf("reads = %v", got)
	}
}

func TestClusterUpgrade_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, seededClusters())), "cluster", "upgrade", "prod", "-l", "FIN-03", "--kubernetes-version", "1.38.0", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	if got["status"] != "accepted" || got["previous_version"] != "1.37.0" {
		t.Errorf("result = %v", got)
	}
	if c, _ := got["cluster"].(map[string]any); c["kubernetes_version"] != "1.38.0" {
		t.Errorf("cluster = %v", got["cluster"])
	}
}

func TestClusterUpgrade_MissingArguments(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "upgrade", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"<cluster>", "--kubernetes-version"}) {
		t.Errorf("missing = %v", missing)
	}
}

// Without --kubernetes-version a terminal asks for it, then shows the warning and confirms.
func TestClusterUpgrade_Interactive(t *testing.T) {
	withTerminal(t)
	api := seededClusters()
	p := tuitest.New().AddTextInput("1.39.0").AddConfirm(true)
	_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "cluster", "upgrade", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "Rolling upgrade from 1.37.0 to 1.39.0") {
		t.Errorf("stderr = %q", errOut)
	}
	patches := api.requestsMatching("PATCH")
	if len(patches) != 1 || decodeJSON[map[string]string](t, patches[0].Body)["kubernetes_version"] != "1.39.0" {
		t.Errorf("PATCHes = %v", patches)
	}
}

func TestClusterUpgrade_APIErrorsShownAsSent(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.failures["PATCH /mk8sclusters/{loc}/{name}"] = apiFailure{Status: 409, Body: `{"code":"conflict","message":"busy","details":"concurrent_modification"}`}
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0", "--yes")
	if err == nil || !strings.Contains(err.Error(), "concurrent_modification") {
		t.Fatalf("err = %v", err)
	}
}

// The version the cluster already has sends nothing, on a terminal too (no confirmation).
func TestClusterUpgrade_SameVersionSendsNothing(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), "cluster", "upgrade", "prod", "--kubernetes-version", "1.37.0", "--yes")
	if r := decodeJSON[ClusterUpgradeResult](t, out); err != nil || r.Status != "unchanged" || r.PreviousVersion != "1.37.0" || r.Cluster.Name != "prod" {
		t.Errorf("result = %+v, err = %v", r, err)
	}
	if patches := api.requestsMatching("PATCH"); len(patches) != 0 {
		t.Errorf("PATCHes = %v", patches)
	}
}

func TestClusterUpgrade_SameVersionInteractive(t *testing.T) {
	withTerminal(t)
	api := seededClusters()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddTextInput("1.37.0")), "cluster", "upgrade", "prod")
	if err != nil || out != "Cluster prod already has Kubernetes version 1.37.0; nothing to do.\n" || len(api.requestsMatching("PATCH")) != 0 {
		t.Errorf("out = %q, err = %v, PATCHes = %v", out, err, api.requestsMatching("PATCH"))
	}
}
