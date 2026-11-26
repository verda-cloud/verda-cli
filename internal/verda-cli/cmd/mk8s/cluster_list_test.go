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
)

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

func TestClusterList_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, seededClusters())), "cluster", "list")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[[]map[string]any](t, out)
	if len(got) != 2 || got[0]["name"] != "prod" || got[1]["phase"] != "provisioning" {
		t.Fatalf("got %v", got)
	}
	for _, k := range []string{
		"name", "location", "kubernetes_version", "phase", "external_endpoint", "network", "subnet",
		"pod_cidr", "service_cidr", "dns_suffix", "conditions", "created_at",
	} {
		if _, ok := got[0][k]; !ok {
			t.Errorf("JSON misses %q", k)
		}
	}
	if c, ok := got[0]["conditions"].([]any); !ok || len(c) != 0 {
		t.Errorf("conditions = %v, want []", got[0]["conditions"])
	}
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

func TestClusterList_Location(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	out, _, err := runMK8s(t, jsonFactory(client), "cluster", "list", "-l", "fin-01")
	if got := decodeJSON[[]ClusterView](t, out); err != nil || len(got) != 1 || got[0].Name != "staging" {
		t.Errorf("-l fin-01: %v, err = %v", got, err)
	}
	out, errOut, err := runMK8s(t, tableFactory(client, nil), "cluster", "list", "-l", "FIN-99")
	if err != nil || out != "No clusters found in FIN-99.\n" || errOut != "" {
		t.Errorf("-l FIN-99: out=%q stderr=%q err=%v", out, errOut, err)
	}
}
