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

//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

const missingDeployment = "verda-it-no-such-deployment"

func TestMCP_ContainerTools_ReadOnly(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	// Gates are checked against a deployment that does not exist: a broken
	// gate then fails with NOT_FOUND instead of touching a real deployment.
	if code := toolErrorCode(t, c.callTool("update_container_scaling", map[string]any{
		"name": missingDeployment, "min_replica_count": 0,
	})); code != "CONFIRMATION_REQUIRED" {
		t.Errorf("update_container_scaling without confirm: code %q, want CONFIRMATION_REQUIRED", code)
	}
	if code := toolErrorCode(t, c.callTool("container_action", map[string]any{
		"name": missingDeployment, "action": "restart",
	})); code != "CONFIRMATION_REQUIRED" {
		t.Errorf("container_action restart without confirm: code %q, want CONFIRMATION_REQUIRED", code)
	}
	if code := toolErrorCode(t, c.callTool("describe_container_deployment", map[string]any{
		"name": missingDeployment,
	})); code != "NOT_FOUND" {
		t.Errorf("describe of a missing deployment: code %q, want NOT_FOUND", code)
	}

	resp := c.callTool("list_container_deployments", map[string]any{})
	assertToolSuccess(t, resp)
	var deployments []map[string]any
	if err := json.Unmarshal([]byte(extractToolText(t, resp)), &deployments); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	t.Logf("list_container_deployments returned %d deployments", len(deployments))
	if len(deployments) == 0 {
		t.Skip("account has no container deployments; describe/replicas not exercised")
	}

	name, _ := deployments[0]["name"].(string)
	d := describeContainer(t, c, name)
	if d["status"] == nil && d["status_error"] == nil {
		t.Errorf("describe has neither status nor status_error: %v", d)
	}
	if d["scaling"] == nil {
		t.Errorf("describe has no scaling (scaling_error: %v)", d["scaling_error"])
	}

	resp = c.callTool("list_container_replicas", map[string]any{"name": name})
	assertToolSuccess(t, resp)
	t.Logf("list_container_replicas(%s): %s", name, extractToolText(t, resp))
}

// --- helpers ---

func describeContainer(t *testing.T, c *mcpClient, name string) map[string]any {
	t.Helper()
	resp := c.callTool("describe_container_deployment", map[string]any{"name": name})
	assertToolSuccess(t, resp)
	text := extractToolText(t, resp)
	if strings.Contains(text, "value_or_reference_to_secret") {
		t.Fatalf("describe leaked env var values: %s", text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse describe: %v", err)
	}
	return out
}

// toolErrorCode returns the agent-contract code of an isError tool result.
func toolErrorCode(t *testing.T, resp map[string]any) string {
	t.Helper()
	assertToolError(t, resp)
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	text := extractToolText(t, resp)
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("error is not the contract envelope: %v\ntext: %s", err, text)
	}
	return env.Error.Code
}
