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

package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// Through-the-protocol tests: the registered tool closures (not the handler
// methods) answer a JSON-RPC tools/call, so registration itself is exercised.

type rpcToolResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func rpc(t *testing.T, s *Server, method string, params any) json.RawMessage {
	t.Helper()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	msg := s.mcpServer.HandleMessage(context.Background(), req)
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error != nil {
		t.Fatalf("%s: JSON-RPC error: %s", method, env.Error.Message)
	}
	return env.Result
}

func rpcCallTool(t *testing.T, s *Server, name string, args map[string]any) rpcToolResult {
	t.Helper()
	var res rpcToolResult
	if err := json.Unmarshal(rpc(t, s, "tools/call", map[string]any{"name": name, "arguments": args}), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s: empty content", name)
	}
	return res
}

// serverlessToolNames is the registered serverless surface; the integration
// suite checks the same list against the built binary.
var serverlessToolNames = []string{
	"list_serverless_compute", "list_registry_credentials", "list_secrets",
	"delete_secret", "delete_file_secret", "delete_registry_credentials",
	"list_containers", "describe_container", "list_container_replicas", "create_container", "container_action",
	"scale_container", "get_container_env", "set_container_env", "unset_container_env",
	"list_batchjobs", "describe_batchjob", "create_batchjob", "batchjob_action",
}

func TestToolsListRegistersServerlessTools(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)
	var list struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc(t, s, "tools/list", map[string]any{}), &list); err != nil {
		t.Fatal(err)
	}
	byName := map[string]bool{}
	for _, tool := range list.Tools {
		byName[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		// Every gated tool declares confirm in its schema so clients can prompt for it.
		props, _ := tool.InputSchema["properties"].(map[string]any)
		switch tool.Name {
		case "create_container", "create_batchjob", "scale_container", "unset_container_env",
			"delete_secret", "delete_file_secret", "delete_registry_credentials", "container_action", "batchjob_action":
			if _, ok := props["confirm"]; !ok {
				t.Errorf("%s must declare a confirm argument", tool.Name)
			}
		}
	}
	for _, name := range serverlessToolNames {
		if !byName[name] {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisteredDeleteClosuresReachTheAPI(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedSecret("s1")
	mock.SeedFileSecret("f1", "a.txt")
	mock.SeedRegistryCredentials("r1")

	for tool, name := range map[string]string{"delete_secret": "s1", "delete_file_secret": "f1", "delete_registry_credentials": "r1"} {
		res := rpcCallTool(t, s, tool, map[string]any{"name": name})
		if !res.IsError {
			t.Errorf("%s without confirm must be refused", tool)
		}
		res = rpcCallTool(t, s, tool, map[string]any{"name": name, "confirm": true})
		if res.IsError {
			t.Errorf("%s: %s", tool, res.Content[0].Text)
		}
	}
	if mock.HasSecret("s1") || mock.HasFileSecret("f1") || mock.HasRegistryCredentials("r1") {
		t.Error("registered delete tools must delete through the API")
	}
}

func TestServerlessToolsOverRPC(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")
	mock.SeedJobDeployment("nightly")

	for tool, args := range map[string]map[string]any{
		"list_serverless_compute":   {},
		"list_registry_credentials": {},
		"list_secrets":              {"kind": "file"},
		"list_containers":           {},
		"describe_container":        {"name": "api"},
		"list_container_replicas":   {"name": "api"},
		"get_container_env":         {"name": "api"},
		"list_batchjobs":            {},
		"describe_batchjob":         {"name": "nightly"},
		"container_action":          {"name": "api", "action": "pause"},
		"batchjob_action":           {"name": "nightly", "action": "pause"},
		"scale_container":           {"name": "api", "confirm": true, "concurrency": float64(2)},
		"set_container_env":         {"name": "api", "env": []any{"A=1"}},
		"unset_container_env":       {"name": "api", "keys": []any{"A"}, "confirm": true},
		"create_container":          containerCreateArgs(nil),
		"create_batchjob":           batchjobCreateArgs(map[string]any{"name": "job2"}),
	} {
		res := rpcCallTool(t, s, tool, args)
		if res.IsError {
			t.Errorf("%s over RPC: %s", tool, res.Content[0].Text)
		}
	}
}
