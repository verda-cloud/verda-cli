//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	stderr *bytes.Buffer
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

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
		stderr: &stderr,
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
		c.t.Fatalf("write request: %v\nstderr: %s", err, c.stderr.String())
	}

	// Read response with timeout
	done := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		done <- line
	}()

	select {
	case line := <-done:
		if line == "" {
			c.t.Fatalf("empty response from MCP server\nstderr: %s", c.stderr.String())
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			c.t.Fatalf("unmarshal response: %v\nraw: %s\nstderr: %s", err, line, c.stderr.String())
		}
		return resp
	case err := <-errCh:
		c.t.Fatalf("read error: %v\nstderr: %s", err, c.stderr.String())
		return nil
	case <-time.After(30 * time.Second):
		c.t.Fatalf("timeout waiting for MCP response\nstderr: %s", c.stderr.String())
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

// initialize performs the MCP handshake.
func (c *mcpClient) initialize() map[string]any {
	c.t.Helper()
	return c.send("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "integration-test", "version": "1.0"},
	})
}

// --- Tests ---

func TestMCP_Handshake(t *testing.T) {
	c := startMCP(t, "test")
	resp := c.initialize()

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
	c.initialize()
	elapsed := time.Since(start)

	// Handshake should complete in under 5 seconds (generous for CI)
	if elapsed > 5*time.Second {
		t.Errorf("handshake took %s, want < 5s", elapsed)
	}
	t.Logf("handshake completed in %s", elapsed)
}

func TestMCP_ToolsList(t *testing.T) {
	c := startMCP(t, "test")
	c.initialize()

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
	c.initialize()

	resp := c.callTool("list_locations", map[string]any{})
	assertToolSuccess(t, resp)

	text := extractToolText(t, resp)
	var locations []map[string]any
	if err := json.Unmarshal([]byte(text), &locations); err != nil {
		t.Fatalf("failed to parse locations JSON: %v", err)
	}
	if len(locations) == 0 {
		t.Fatal("expected at least one location")
	}
	t.Logf("MCP list_locations returned %d locations", len(locations))
}

func TestMCP_ListInstanceTypes(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("list_instance_types", map[string]any{"gpu_only": true})
	assertToolSuccess(t, resp)

	text := extractToolText(t, resp)
	var types []map[string]any
	if err := json.Unmarshal([]byte(text), &types); err != nil {
		t.Fatalf("failed to parse instance types JSON: %v", err)
	}
	if len(types) == 0 {
		t.Fatal("expected at least one GPU instance type")
	}
	t.Logf("MCP list_instance_types (gpu_only) returned %d types", len(types))
}

func TestMCP_GetBalance(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("get_balance", map[string]any{})
	assertToolSuccess(t, resp)
	t.Log("MCP get_balance OK")
}

func TestMCP_EstimateCost(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("estimate_cost", map[string]any{
		"instance_type": "1A6000.10V",
		"os_volume_gb":  100,
	})
	assertToolSuccess(t, resp)

	text := extractToolText(t, resp)
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
	c.initialize()

	resp := c.callTool("estimate_cost", map[string]any{})
	assertToolError(t, resp)
}

func TestMCP_ListVMs(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("list_vms", map[string]any{})
	assertToolSuccess(t, resp)
	t.Log("MCP list_vms OK")
}

func TestMCP_DescribeVM_InvalidID(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("describe_vm", map[string]any{"id": "nonexistent-id-xyz"})
	assertToolError(t, resp)
	t.Log("MCP describe_vm correctly returned error for invalid ID")
}

func TestMCP_ListSSHKeys(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("list_ssh_keys", map[string]any{})
	assertToolSuccess(t, resp)
	t.Log("MCP list_ssh_keys OK")
}

func TestMCP_ListVolumes(t *testing.T) {
	requireProfile(t, "test")
	c := startMCP(t, "test")
	c.initialize()

	resp := c.callTool("list_volumes", map[string]any{})
	assertToolSuccess(t, resp)
	t.Log("MCP list_volumes OK")
}

func TestMCP_AuthError_NoCredentials(t *testing.T) {
	c := startMCP(t, "test-empty")
	c.initialize()

	resp := c.callTool("list_locations", map[string]any{})
	assertToolError(t, resp)

	text := extractToolText(t, resp)
	t.Logf("error for no credentials: %s", text)
}

func TestMCP_AuthError_InvalidCredentials(t *testing.T) {
	c := startMCP(t, "test-invalid")
	c.initialize()

	resp := c.callTool("list_locations", map[string]any{})
	assertToolError(t, resp)

	text := extractToolText(t, resp)
	if text == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("error for invalid credentials: %s", text)
}

// --- Helpers ---

// assertToolSuccess verifies a tool call returned successfully.
func assertToolSuccess(t *testing.T, resp map[string]any) {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]any)
		t.Fatalf("tool returned error: %v", content)
	}
}

// assertToolError verifies a tool call returned an error.
func assertToolError(t *testing.T, resp map[string]any) {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got %v", resp)
	}
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatal("expected isError=true")
	}
}

// extractToolText extracts the text content from a tool result.
func extractToolText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	textContent, _ := content[0].(map[string]any)
	text, _ := textContent["text"].(string)
	return text
}
