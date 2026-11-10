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

package serverless

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestContainerReplicas(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, &cmdutil.TestFactory{ClientOverride: client}, "replicas", testDeployment)
		if err != nil {
			t.Fatalf("replicas: %v", err)
		}
		for _, want := range []string{"ID", "STATUS", "STARTED", "IMAGE", "r-1", "running", "2026-08-09 01:00:00", "1 replica(s): 1 running"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, agentFactory(client), "replicas", testDeployment)
		if err != nil {
			t.Fatalf("replicas: %v", err)
		}
		var res containerReplicasResult
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if res.Name != testDeployment || res.Count != 1 || res.CountsByStatus["running"] != 1 || res.Replicas[0].ID != "r-1" {
			t.Errorf("result = %+v", res)
		}
	})

	t.Run("agent requires a name", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		if _, _, err := runContainer(t, agentFactory(client), "replicas"); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
			t.Errorf("error = %v, want MISSING_REQUIRED_FLAGS", err)
		}
	})
}

func TestReplicaImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rep  verda.ReplicaInfo
		want string
	}{
		{"full image wins", verda.ReplicaInfo{Image: "ghcr.io/org/app:v2", ImageName: "ignored", ImageTag: "ignored"}, "ghcr.io/org/app:v2"},
		{"name+tag", verda.ReplicaInfo{ImageName: "nginx", ImageTag: "1.21"}, "nginx:1.21"},
		{"name only", verda.ReplicaInfo{ImageName: "nginx"}, "nginx"},
		{"all empty", verda.ReplicaInfo{}, ""},
		{"tag without name", verda.ReplicaInfo{ImageTag: "v1"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := replicaImage(&tt.rep); got != tt.want {
				t.Errorf("replicaImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintReplicaTable_Empty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := &containerReplicasResult{Name: "test-deploy", CountsByStatus: map[string]int{}}
	if err := printReplicaTable(&buf, r); err != nil {
		t.Fatalf("printReplicaTable: %v", err)
	}
	if !strings.Contains(buf.String(), "No replicas running") {
		t.Errorf("expected empty message, got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "test-deploy") {
		t.Errorf("expected deployment name in message, got:\n%s", buf.String())
	}
}

func TestPrintReplicaTable_ZeroStartTime(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := &containerReplicasResult{
		Name:  "deploy",
		Count: 1,
		CountsByStatus: map[string]int{
			"initializing": 1,
		},
		Replicas: []verda.ReplicaInfo{{
			ID:     "r-1",
			Status: "initializing",
			Image:  "app:v1",
		}},
	}
	if err := printReplicaTable(&buf, r); err != nil {
		t.Fatalf("printReplicaTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "-") {
		t.Errorf("expected dash for zero start time, got:\n%s", out)
	}
	if !strings.Contains(out, "1 replica(s): 1 initializing") {
		t.Errorf("expected summary line, got:\n%s", out)
	}
}

func TestPrintReplicaTable_MultipleStatuses(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := &containerReplicasResult{
		Name:  "multi",
		Count: 3,
		CountsByStatus: map[string]int{
			"error":   1,
			"running": 2,
		},
		Replicas: []verda.ReplicaInfo{
			{ID: "r-1", Status: "running", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Image: "app:v1"},
			{ID: "r-2", Status: "running", StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Image: "app:v1"},
			{ID: "r-3", Status: "error", Image: "app:v1"},
		},
	}
	if err := printReplicaTable(&buf, r); err != nil {
		t.Fatalf("printReplicaTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "3 replica(s): 1 error, 2 running") {
		t.Errorf("expected sorted summary, got:\n%s", out)
	}
}
