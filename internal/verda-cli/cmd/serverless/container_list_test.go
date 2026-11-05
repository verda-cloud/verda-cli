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
	"slices"
	"testing"
)

func TestContainerList_StatusFilterIsExact(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	mock.SeedContainerDeployment("sick-api")
	mock.SetContainerStatus("sick-api", "unhealthy")

	for status, want := range map[string][]string{
		"healthy":   {testDeployment},
		"UNHEALTHY": {"sick-api"},
		"health":    nil,
	} {
		stdout, _, err := runContainer(t, agentFactory(client), "list", "--status", status)
		if err != nil {
			t.Fatalf("list --status %s: %v", status, err)
		}
		var rows []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("--status %s: got %v, want %v", status, got, want)
		}
	}
}
