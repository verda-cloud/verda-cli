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
	"net/http"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type describeJSON struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Scaling *struct {
		MaxReplicaCount int `json:"max_replica_count"`
	} `json:"scaling"`
	ScalingError string `json:"scaling_error"`
}

func TestContainerDescribe_Scaling(t *testing.T) {
	t.Run("table shows scaling", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, &cmdutil.TestFactory{ClientOverride: client}, "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		for _, want := range []string{"Scaling", "0..3", "balanced (3)", "on at 70%", "5m"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("json includes scaling", func(t *testing.T) {
		t.Parallel()
		_, client := newContainerMock(t)
		stdout, _, err := runContainer(t, agentFactory(client), "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		var res describeJSON
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if res.Scaling == nil || res.Scaling.MaxReplicaCount != 3 || res.ScalingError != "" {
			t.Errorf("result = %+v, want scaling with max 3", res)
		}
	})

	t.Run("scaling failure is best-effort", func(t *testing.T) {
		t.Parallel()
		mock, client := newContainerMock(t)
		mock.FailRoute("/container-deployments/"+testDeployment+"/scaling", http.StatusInternalServerError)

		stdout, _, err := runContainer(t, agentFactory(client), "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe: %v", err)
		}
		var res describeJSON
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		if res.Name != testDeployment || res.Status == "" || res.Scaling != nil || res.ScalingError == "" {
			t.Errorf("result = %+v, want the deployment and status with scaling_error", res)
		}

		stdout, _, err = runContainer(t, &cmdutil.TestFactory{ClientOverride: client}, "describe", testDeployment)
		if err != nil {
			t.Fatalf("describe (table): %v", err)
		}
		if !strings.Contains(stdout, "Scaling") || !strings.Contains(stdout, "unavailable") {
			t.Errorf("table lacks the scaling note:\n%s", stdout)
		}
	})
}
