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
	"encoding/json"
	"strings"
	"testing"

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
		for _, want := range []string{"ID", "STATUS", "STARTED", "r-1", "running", "2026-08-09 01:00:00", "1 replica(s): 1 running"} {
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
