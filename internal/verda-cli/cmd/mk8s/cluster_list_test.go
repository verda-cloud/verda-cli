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
	"time"

	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func seededClusters() *fakeAPI {
	api := newFakeAPI()
	api.addCluster("prod", "FIN-03", "running")
	api.addCluster("staging", "FIN-01", "provisioning")
	return api
}

func TestClusterList_Table(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and 2 rows, got:\n%s", out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "NAME LOCATION VERSION PHASE ENDPOINT AGE" {
		t.Errorf("header = %q", got)
	}
	prod := strings.Fields(lines[1])
	if prod[0] != "prod" || prod[1] != "FIN-03" || prod[2] != "1.37.0" || prod[3] != "running" || prod[4] != "prod.example.test:6443" {
		t.Errorf("prod row = %v", prod)
	}
	if staging := strings.Fields(lines[2]); staging[3] != "provisioning" || staging[4] != "-" {
		t.Errorf("a cluster without an endpoint shows '-': %v", staging)
	}
	if got := api.paths("GET"); len(got) != 1 || got[0] != "/mk8sclusters" {
		t.Errorf("want one List call, got %v", got)
	}
	if len(api.requestsMatching("POST"))+len(api.requestsMatching("DELETE"))+len(api.requestsMatching("PATCH")) != 0 {
		t.Error("list must not write")
	}
}

func TestClusterList_LongEndpointsDropTheColumnToFit(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.clusters[0].ExternalEndpoint = "https://prod.project-824e2c70-bfce-4227-9ca4-369fc72d9280.fin03.mk8s.example.test:9443"
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ENDPOINT") || strings.Contains(out, "mk8s.example.test") {
		t.Errorf("the endpoint column must be dropped when rows exceed %d columns:\n%s", clusterTableWidth, out)
	}
	if got := widestLine(strings.TrimRight(out, "\n")); got > clusterTableWidth {
		t.Errorf("widest line = %d", got)
	}
	jsonOut, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut, "https://prod.project-824e2c70") {
		t.Errorf("JSON keeps the full endpoint:\n%s", jsonOut)
	}
}

func TestClusterList_Structured(t *testing.T) {
	t.Parallel()
	wantKeys := []string{
		"name", "location", "kubernetes_version", "phase", "external_endpoint", "network", "subnet",
		"pod_cidr", "service_cidr", "dns_suffix", "conditions", "created_at",
	}
	for name, mk := range map[string]func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory{
		"json":  func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"agent": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _, err := runMK8s(t, mk(seededClusters(), t), "cluster", "list")
			if err != nil {
				t.Fatal(err)
			}
			got := decodeJSON[[]map[string]any](t, out)
			if len(got) != 2 || got[0]["name"] != "prod" || got[1]["phase"] != "provisioning" {
				t.Fatalf("got %v", got)
			}
			for _, k := range wantKeys {
				if _, ok := got[0][k]; !ok {
					t.Errorf("JSON misses %q", k)
				}
			}
			if c, ok := got[0]["conditions"].([]any); !ok || len(c) != 0 {
				t.Errorf("conditions = %v, want []", got[0]["conditions"])
			}
		})
	}

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()
		out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, seededClusters())), "cluster", "list")
		if err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0]["kubernetes_version"] != "1.37.0" {
			t.Errorf("got %v", got)
		}
	})
}

func TestClusterList_Empty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, newFakeAPI())

	out, errOut, err := runMK8s(t, tableFactory(client, nil), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No clusters found.") || !strings.Contains(errOut, "verda mk8s cluster create") {
		t.Errorf("stdout=%q stderr=%q", out, errOut)
	}

	out, _, err = runMK8s(t, jsonFactory(client), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("empty JSON = %q, want []", out)
	}
}

func TestClusterList_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, seededClusters()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "API response: 2 cluster(s):") || !strings.Contains(errOut, `"kubernetes_version": "1.37.0"`) {
		t.Errorf("debug output = %q", errOut)
	}
}

func TestClusterList_NotEnabled(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	api.disabled = true
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "list")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgeColumn(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "30s"}, {5 * time.Minute, "5m"}, {3 * time.Hour, "3h"}, {49 * time.Hour, "2d"}, {-time.Minute, "0s"},
	} {
		if got := ageColumn(now.Add(-c.ago), now); got != c.want {
			t.Errorf("ageColumn(%v ago) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := ageColumn(time.Time{}, now); got != "-" {
		t.Errorf("zero time = %q", got)
	}
}
