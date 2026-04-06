//go:build integration

package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"
)

// mcpClient manages a stdio connection to the MCP server.
type mcpClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	t      *testing.T
	nextID int
}

// startMCP starts the MCP server as a subprocess.
func startMCP(t *testing.T, profile string) *mcpClient {
	t.Helper()
	args := []string{"mcp", "serve"}
	if profile != "" {
		args = append([]string{"--auth.profile", profile}, args...)
	}
	cmd := exec.Command(verdaBin(), args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = nil // discard stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	return &mcpClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		t:      t,
		nextID: 1,
	}
}

// send sends a JSON-RPC request and returns the parsed response.
func (c *mcpClient) send(method string, params any) map[string]any {
	c.t.Helper()
	id := c.nextID
	c.nextID++

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, _ := json.Marshal(req)
	b = append(b, '\n')

	if _, err := c.stdin.Write(b); err != nil {
		c.t.Fatalf("write request: %v", err)
	}

	// Read response with timeout
	done := make(chan string, 1)
	go func() {
		line, _ := c.stdout.ReadString('\n')
		done <- line
	}()

	select {
	case line := <-done:
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			c.t.Fatalf("unmarshal response: %v\nraw: %s", err, line)
		}
		return resp
	case <-time.After(30 * time.Second):
		c.t.Fatal("timeout waiting for MCP response")
		return nil
	}
}

// callTool sends a tools/call request and returns the result.
func (c *mcpClient) callTool(name string, args map[string]any) map[string]any {
	c.t.Helper()
	return c.send("tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

func TestMCP_Handshake(t *testing.T) {
	c := startMCP(t, "test")

	resp := c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}

	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("missing serverInfo")
	}
	if name, _ := serverInfo["name"].(string); name != "verda-cloud" {
		t.Errorf("server name = %q, want verda-cloud", name)
	}
	t.Logf("MCP handshake OK: %v", serverInfo)
}

func TestMCP_HandshakeSpeed(t *testing.T) {
	start := time.Now()
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})
	elapsed := time.Since(start)

	// Handshake should complete in under 5 seconds (generous for CI)
	if elapsed > 5*time.Second {
		t.Errorf("handshake took %s, want < 5s", elapsed)
	}
	t.Logf("handshake completed in %s", elapsed)
}

func TestMCP_ToolsList(t *testing.T) {
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.send("tools/list", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}

	expectedTools := []string{
		"list_locations", "list_instance_types", "check_availability", "list_images",
		"get_balance", "estimate_cost", "get_running_costs",
		"list_vms", "describe_vm", "create_vm", "vm_action",
		"list_ssh_keys", "add_ssh_key", "get_ssh_command",
		"list_volumes", "create_volume", "list_volumes_in_trash",
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		if tm, ok := tool.(map[string]any); ok {
			if name, ok := tm["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("missing tool %q", expected)
		}
	}
	t.Logf("found %d tools", len(tools))
}

func TestMCP_ListLocations(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("list_locations", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]any)
		t.Fatalf("tool returned error: %v", content)
	}

	// Parse the text content as JSON
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("empty content")
	}
	textContent, ok := content[0].(map[string]any)
	if !ok {
		t.Fatal("expected text content")
	}
	text, _ := textContent["text"].(string)

	var locations []map[string]any
	if err := json.Unmarshal([]byte(text), &locations); err != nil {
		t.Fatalf("failed to parse locations JSON: %v", err)
	}
	if len(locations) == 0 {
		t.Fatal("expected at least one location")
	}
	t.Logf("MCP list_locations returned %d locations", len(locations))
}

func TestMCP_GetBalance(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("get_balance", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]any)
		t.Fatalf("tool returned error: %v", content)
	}
	t.Logf("MCP get_balance OK")
}

func TestMCP_AuthError_NoCredentials(t *testing.T) {
	c := startMCP(t, "test-empty")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("list_locations", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}

	// Should return an error
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatal("expected isError=true for missing credentials")
	}

	content, _ := result["content"].([]any)
	if len(content) > 0 {
		textContent, _ := content[0].(map[string]any)
		text, _ := textContent["text"].(string)
		t.Logf("error message: %s", text)
	}
}

func TestMCP_AuthError_InvalidCredentials(t *testing.T) {
	c := startMCP(t, "test-invalid")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("list_locations", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}

	// Should return an error
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatal("expected isError=true for invalid credentials")
	}

	content, _ := result["content"].([]any)
	if len(content) > 0 {
		textContent, _ := content[0].(map[string]any)
		text, _ := textContent["text"].(string)
		t.Logf("error message: %s", text)
		if text == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestMCP_DescribeVM_InvalidID(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("describe_vm", map[string]any{"id": "nonexistent-id-xyz"})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}

	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatal("expected isError=true for invalid instance ID")
	}
	t.Log("MCP describe_vm correctly returned error for invalid ID")
}

func TestMCP_EstimateCost(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	resp := c.callTool("estimate_cost", map[string]any{
		"instance_type": "1V100.6V",
		"os_volume_gb":  100,
	})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]any)
		t.Fatalf("tool returned error: %v", content)
	}

	// Parse the estimate from content
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("empty content")
	}
	textContent, _ := content[0].(map[string]any)
	text, _ := textContent["text"].(string)

	var estimate map[string]any
	if err := json.Unmarshal([]byte(text), &estimate); err != nil {
		t.Fatalf("failed to parse estimate: %v", err)
	}

	est, ok := estimate["estimate"].(map[string]any)
	if !ok {
		t.Fatal("missing 'estimate' in response")
	}
	for _, field := range []string{"hourly", "daily", "monthly"} {
		if _, ok := est[field]; !ok {
			t.Errorf("missing %q in estimate", field)
		}
	}
	t.Logf("MCP estimate_cost: %v", est)
}

func TestMCP_EstimateCost_MissingRequired(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")

	c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
	})

	// Call without required instance_type
	resp := c.callTool("estimate_cost", map[string]any{})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}

	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatal("expected error when instance_type is missing")
	}

	content, _ := result["content"].([]any)
	if len(content) > 0 {
		textContent, _ := content[0].(map[string]any)
		text, _ := textContent["text"].(string)
		fmt.Println(text)
		if text == "" {
			t.Error("expected error message about missing instance_type")
		}
	}
}
