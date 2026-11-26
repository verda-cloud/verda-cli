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
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// kcPrivateKeyMarker stands for the client key in the fake kubeconfig YAML: it must never be printed.
const kcPrivateKeyMarker = "MARKER-PRIVATE-KEY"

var kcExpiry = time.Date(2027, 2, 17, 9, 30, 0, 0, time.UTC)

// kcFixtureAPI has the clusters of seededClusters and two kubeconfigs in "prod": a
// ready one with groups and an expiry, and a pending one with neither.
func kcFixtureAPI() *fakeAPI {
	api := seededClusters()
	api.addKubeconfig("prod", verda.ManagedKubernetesKubeconfig{
		ID: "prod-kc-aaa", DisplayName: "alice-laptop", CommonName: "alice", Groups: []string{"system:masters", "dev"},
		Status: verda.ManagedKubernetesKubeconfigStatusReady, NotAfter: &kcExpiry,
		Kubeconfig: "apiVersion: v1\nusers:\n- user:\n    client-key-data: " + kcPrivateKeyMarker + "\n",
	})
	api.addKubeconfig("prod", verda.ManagedKubernetesKubeconfig{
		ID: "prod-kc-bbb", CommonName: "ci-bot", Status: verda.ManagedKubernetesKubeconfigStatusPending,
		Kubeconfig: "client-key-data: " + kcPrivateKeyMarker,
	})
	return api
}

func kcWantViews() []KubeconfigView {
	return []KubeconfigView{
		{ID: "prod-kc-aaa", DisplayName: "alice-laptop", CommonName: "alice", Groups: []string{"system:masters", "dev"}, Status: "ready", NotAfter: &kcExpiry},
		{ID: "prod-kc-bbb", CommonName: "ci-bot", Groups: []string{}, Status: "pending"},
	}
}

func TestKubeconfigList_Table(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "list", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and 2 rows:\n%s", out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ID DISPLAY NAME COMMON NAME GROUPS STATUS EXPIRES" {
		t.Errorf("header = %q", got)
	}
	want := [][]string{
		{"prod-kc-aaa", "alice-laptop", "alice", "system:masters,dev", "ready", "2027-02-17", "09:30", "UTC"},
		{"prod-kc-bbb", "-", "ci-bot", "-", "pending", "-"},
	}
	for i, w := range want {
		if got := strings.Fields(lines[i+1]); !slices.Equal(got, w) {
			t.Errorf("row %d = %v, want %v", i, got, w)
		}
	}
	wantReqs := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs"}
	if got := api.paths("GET"); !slices.Equal(got, wantReqs) {
		t.Errorf("requests = %v, want %v", got, wantReqs)
	}
}

// Every key but file is always present: a pending kubeconfig has groups [] and not_after null.
func TestKubeconfigList_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "list", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeJSON[[]KubeconfigView](t, out); !reflect.DeepEqual(got, kcWantViews()) {
		t.Errorf("got  %+v\nwant %+v", got, kcWantViews())
	}
	for i, m := range decodeJSON[[]map[string]any](t, out) {
		if len(m) != 6 {
			t.Errorf("item %d = %v, want the 6 keys of the view", i, m)
		}
		for _, k := range []string{"common_name", "display_name", "groups", "id", "not_after", "status"} {
			if _, ok := m[k]; !ok {
				t.Errorf("item %d misses %q", i, k)
			}
		}
	}
}

func TestKubeconfigList_Empty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	out, errOut, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "--cluster", "prod")
	if err != nil || !strings.Contains(out, "No kubeconfigs in cluster prod.") || !strings.Contains(errOut, "verda mk8s kubeconfig create --cluster prod") {
		t.Errorf("out=%q err=%q error=%v", out, errOut, err)
	}
	out, _, err = runMK8s(t, jsonFactory(client), "kubeconfig", "list", "--cluster", "prod")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("json: empty output = %q, error=%v", out, err)
	}
}

func TestKubeconfigList_ClusterErrors(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, kcFixtureAPI())
	_, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "list")
	if m, _ := cmdutil.ClassifyError(err).Details["missing"].([]string); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" || !slices.Equal(m, []string{"--cluster"}) {
		t.Errorf("err = %v, missing = %v", err, m)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "--cluster", "ghost"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
}
