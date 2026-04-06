package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	pkgversion "github.com/verda-cloud/verdagostack/pkg/version"
)

// Server wraps the MCP protocol server and Verda SDK client.
type Server struct {
	client    *verda.Client
	mcpServer *server.MCPServer
}

// NewServer creates a new MCP server backed by the given Verda client.
func NewServer(client *verda.Client) *Server {
	s := &Server{client: client}

	ver := pkgversion.Get().GitVersion
	s.mcpServer = server.NewMCPServer(
		"verda-cloud",
		ver,
	)

	s.registerDiscoveryTools()
	s.registerCostTools()
	s.registerVMTools()
	s.registerSSHTools()
	s.registerVolumeTools()

	return s
}

// ServeStdio starts the MCP server on stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	stdio := server.NewStdioServer(s.mcpServer)
	return stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// jsonResult is a helper that marshals data as a JSON text tool result.
func jsonResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

// args extracts the arguments map from a CallToolRequest.
//
//nolint:gocritic // hugeParam: handler signature is defined by mcp-go library.
func args(req mcp.CallToolRequest) map[string]any {
	return req.GetArguments()
}

// requiredString extracts a required string argument.
func requiredString(a map[string]any, name string) (string, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", name)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("argument %q must be a non-empty string", name)
	}
	return s, nil
}

// optionalString extracts an optional string argument, returning "" if absent.
func optionalString(a map[string]any, name string) string {
	v, ok := a[name]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// optionalBool extracts an optional boolean argument, returning false if absent.
func optionalBool(a map[string]any, name string) bool {
	v, ok := a[name]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// optionalInt extracts an optional integer argument, returning 0 if absent.
func optionalInt(a map[string]any, name string) int {
	v, ok := a[name]
	if !ok || v == nil {
		return 0
	}
	// JSON numbers are float64
	f, ok := v.(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// optionalStringSlice extracts an optional string array argument.
func optionalStringSlice(a map[string]any, name string) []string {
	v, ok := a[name]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
