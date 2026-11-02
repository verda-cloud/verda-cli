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
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// liveContainerEnv names a deployment that exists only for
// TestMCP_ContainerScaling_Live: the test pauses, restarts, purges and
// rescales it. Never point it at a deployment that serves traffic.
const liveContainerEnv = "VERDA_IT_CONTAINER_DEPLOYMENT"

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

func TestMCP_ContainerScaling_Live(t *testing.T) {
	name := os.Getenv(liveContainerEnv)
	if name == "" {
		t.Skip(liveContainerEnv + " not set; this test rescales and restarts that deployment")
	}
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.timeout = 90 * time.Second // update_container_scaling wait polls up to 60 s
	c.initialize()

	orig := containerScaling(t, c, name)
	t.Logf("original scaling: %v", orig)
	// Registered after startMCP, so it runs before the server is killed.
	t.Cleanup(func() { restoreScaling(t, c, name, orig) })

	t.Run("max only keeps min and triggers", func(t *testing.T) {
		maxR := num(orig["max_replica_count"])
		if maxR >= 1000 {
			t.Skip("max already at the limit")
		}
		updateScaling(t, c, name, map[string]any{"max_replica_count": maxR + 1})
		now := containerScaling(t, c, name)
		assertSame(t, "min_replica_count", orig["min_replica_count"], now["min_replica_count"])
		assertSame(t, "scaling_triggers", orig["scaling_triggers"], now["scaling_triggers"])
	})

	// CPU, not GPU: the API rejects a GPU trigger on CPU compute.
	t.Run("cpu trigger can be turned off and on", func(t *testing.T) {
		updateScaling(t, c, name, map[string]any{"cpu_utilization_enabled": false})
		if on, _ := trigger(containerScaling(t, c, name), "cpu_utilization")["enabled"].(bool); on {
			t.Error("cpu trigger still enabled after disabling")
		}
		updateScaling(t, c, name, map[string]any{"cpu_utilization_threshold": 75})
		g := trigger(containerScaling(t, c, name), "cpu_utilization")
		if on, _ := g["enabled"].(bool); !on || num(g["threshold"]) != 75 {
			t.Errorf("cpu trigger after enabling = %v, want enabled at 75", g)
		}
	})

	t.Run("pause and resume", func(t *testing.T) {
		containerAction(t, c, name, "pause", false)
		containerAction(t, c, name, "resume", false)
	})

	t.Run("restart and purge_queue with confirm", func(t *testing.T) {
		containerAction(t, c, name, "restart", true)
		containerAction(t, c, name, "purge_queue", true)
	})
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

func containerScaling(t *testing.T, c *mcpClient, name string) map[string]any {
	t.Helper()
	sc, ok := describeContainer(t, c, name)["scaling"].(map[string]any)
	if !ok {
		t.Fatalf("describe returned no scaling for %s", name)
	}
	return sc
}

// updateScaling waits for the change: production reads lag it.
func updateScaling(t *testing.T, c *mcpClient, name string, a map[string]any) {
	t.Helper()
	a["name"] = name
	a["confirm"] = true
	a["wait"] = true
	var resp map[string]any
	// Production answers 503 "Failed to check quota. Try again" now and then.
	for attempt := 1; ; attempt++ {
		resp = c.callTool("update_container_scaling", a)
		if attempt == 3 || !strings.Contains(extractToolText(t, resp), `"status":503`) {
			break
		}
		t.Logf("update_container_scaling got 503, retrying (%d/3)", attempt)
		time.Sleep(5 * time.Second)
	}
	assertToolSuccess(t, resp)
	var out map[string]any
	if err := json.Unmarshal([]byte(extractToolText(t, resp)), &out); err != nil {
		t.Fatalf("parse update result: %v", err)
	}
	if out["status"] != "applied" {
		t.Errorf("update_container_scaling %v: status %v, wait_error %v", a, out["status"], out["wait_error"])
	}
	t.Logf("update_container_scaling %v: %v", a, out["status"])
}

func containerAction(t *testing.T, c *mcpClient, name, action string, confirm bool) {
	t.Helper()
	resp := c.callTool("container_action", map[string]any{"name": name, "action": action, "confirm": confirm})
	assertToolSuccess(t, resp)
	t.Logf("container_action %s: %s", action, extractToolText(t, resp))
}

// restoreScaling puts back every option the live test may have changed.
func restoreScaling(t *testing.T, c *mcpClient, name string, orig map[string]any) {
	t.Helper()
	a := map[string]any{
		"min_replica_count":               orig["min_replica_count"],
		"max_replica_count":               orig["max_replica_count"],
		"concurrent_requests_per_replica": orig["concurrent_requests_per_replica"],
		"queue_message_ttl_seconds":       orig["queue_message_ttl_seconds"],
		"queue_load_threshold":            trigger(orig, "queue_load")["threshold"],
	}
	for _, p := range []string{"cpu", "gpu"} {
		tr := trigger(orig, p+"_utilization")
		if on, _ := tr["enabled"].(bool); on {
			a[p+"_utilization_threshold"] = tr["threshold"]
		} else {
			a[p+"_utilization_enabled"] = false
		}
	}
	maps.DeleteFunc(a, func(_ string, v any) bool { return v == nil })
	updateScaling(t, c, name, a)
}

func trigger(scaling map[string]any, name string) map[string]any {
	triggers, _ := scaling["scaling_triggers"].(map[string]any)
	tr, _ := triggers[name].(map[string]any)
	return tr
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func assertSame(t *testing.T, field string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s changed:\n got  %v\n want %v", field, got, want)
	}
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
