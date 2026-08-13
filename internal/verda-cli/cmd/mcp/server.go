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
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	pkgversion "github.com/verda-cloud/verda-cli/pkg/version"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// clientFunc is a function that returns a Verda client on demand.
type clientFunc func() (*verda.Client, error)

// Server wraps the MCP protocol server and Verda SDK client.
type Server struct {
	client     *verda.Client
	clientErr  error
	clientOnce sync.Once
	getClient  clientFunc
	mcpServer  *server.MCPServer
}

// NewServer creates a new MCP server backed by the given Verda client.
func NewServer(client *verda.Client) *Server {
	return newServer(func() (*verda.Client, error) { return client, nil })
}

// NewLazyServer creates an MCP server that defers client creation to the
// first tool call. This allows the MCP handshake to complete instantly.
func NewLazyServer(getClient clientFunc) *Server {
	return newServer(getClient)
}

// serverInstructions carries the confirm gate and the error envelope — the two
// contracts that decide whether an agent spends money correctly. It is prompt
// context in every session, so keep it short.
const serverInstructions = `Verda Cloud: GPU/CPU instances, volumes, SSH keys, object storage.

CONFIRM GATE — tools that create billing or destructive changes (create_vm,
create_volume, and vm_action with shutdown/force_shutdown/hibernate/delete)
refuse to run unless you pass confirm: true. Show the user the exact target and
its cost first, then retry with confirm. A refused call has no side effects.

ERRORS — a failed tool returns isError with a JSON text payload:
  {"error": {"code": "...", "message": "...", "details": {...}}}
Branch on code, not on message text. Codes you should handle:
  CONFIRMATION_REQUIRED   - retry with confirm: true after telling the user
  MISSING_REQUIRED_FLAGS  - details.missing lists the arguments to supply
  VALIDATION_ERROR        - details.field + details.reason
  SSH_KEY_REQUIRED        - call list_ssh_keys, retry with ssh_key_ids
  AUTH_ERROR              - credentials problem; the user must fix them, not you
  NOT_FOUND               - re-list to find the correct id
  INSUFFICIENT_BALANCE    - stop and tell the user; do not retry
  API_ERROR               - upstream failure; details.status has the HTTP status
details.api_message, when present, is the upstream text kept verbatim.

STATUS HONESTY — create/action tools return status "accepted" unless you pass
wait: true, which polls and returns "completed". Never tell the user a resource
is ready on an "accepted" result.

PRICING — every figure these tools return is an estimate from the catalog. It
cannot see credits, discounts or contract terms, and you may misread or
miscalculate it. Never present a number as the amount the user will be charged,
never sum or convert figures for them without saying you did, and always point
them at the web console, which is authoritative for charges.`

func newServer(getClient clientFunc) *Server {
	s := &Server{getClient: getClient}

	ver := pkgversion.Get().GitVersion
	s.mcpServer = server.NewMCPServer(
		"verda-cloud",
		ver,
		// Rides the initialize response: clients learn the contracts before
		// their first tool call, with no client-side change.
		server.WithInstructions(serverInstructions),
	)

	s.registerDiscoveryTools()
	s.registerCostTools()
	s.registerVMTools()
	s.registerSSHTools()
	s.registerVolumeTools()

	return s
}

// verdaClient returns the Verda SDK client, creating it exactly once.
// mcp-go dispatches tool calls on a worker pool, so the lazy init must be
// safe for concurrent first calls; a factory error is latched too (fix
// credentials, then restart the server).
func (s *Server) verdaClient() (*verda.Client, error) {
	s.clientOnce.Do(func() {
		s.client, s.clientErr = s.getClient()
	})
	if s.clientErr != nil {
		return nil, s.clientErr
	}
	return s.client, nil
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

// Argument errors are cmdutil.AgentError values: one error type across both
// surfaces (docs/agent-errors.md). Wording is MCP's ("argument", not "flag");
// codes and details are the shared contract. ExitCode is inert over MCP.

func missingArgError(name string) *cmdutil.AgentError {
	return &cmdutil.AgentError{
		Code:     "MISSING_REQUIRED_FLAGS",
		Message:  fmt.Sprintf("missing required argument %q", name),
		Details:  map[string]any{"missing": []string{name}},
		ExitCode: cmdutil.ExitBadArgs,
	}
}

func invalidArgError(name, reason string) *cmdutil.AgentError {
	return &cmdutil.AgentError{
		Code:     "VALIDATION_ERROR",
		Message:  fmt.Sprintf("invalid value for %s: %s", name, reason),
		Details:  map[string]any{"field": name, "reason": reason},
		ExitCode: cmdutil.ExitBadArgs,
	}
}

// confirmationRequiredError mirrors the CLI's agent-mode CONFIRMATION_REQUIRED
// contract: destructive and billing tools refuse to run without confirm=true.
func confirmationRequiredError(action string) *cmdutil.AgentError {
	return &cmdutil.AgentError{
		Code:     "CONFIRMATION_REQUIRED",
		Message:  fmt.Sprintf("action %q creates billing or destructive changes and requires an explicit confirm: true argument", action),
		Details:  map[string]any{"action": action},
		ExitCode: cmdutil.ExitBadArgs,
	}
}

// toolErrorResult renders any error as the agent-contract envelope
// ({"error": {code, message, details}}) via cmdutil.ClassifyError — the CLI's
// funnel — so a code added there reaches MCP clients without a change here.
func toolErrorResult(err error) *mcp.CallToolResult {
	ae := cmdutil.ClassifyError(err)
	if ae == nil {
		return mcp.NewToolResultError("unknown error")
	}
	b, mErr := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    ae.Code,
			"message": ae.Message,
			"details": ae.Details,
		},
	})
	if mErr != nil {
		return mcp.NewToolResultError(ae.Message)
	}
	return mcp.NewToolResultError(string(b))
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
		return "", missingArgError(name)
	}
	s, ok := v.(string)
	if !ok {
		return "", invalidArgError(name, "must be a string, got "+jsonTypeName(v))
	}
	if s == "" {
		return "", invalidArgError(name, "must be a non-empty string")
	}
	return s, nil
}

// optionalString extracts an optional string argument. A present value of the
// wrong type is rejected: mcp-go does no schema validation, so silent coercion
// here (e.g. treating a number as "") hides caller bugs.
func optionalString(a map[string]any, name string) (string, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", invalidArgError(name, "must be a string, got "+jsonTypeName(v))
	}
	return s, nil
}

// optionalBool extracts an optional boolean argument, returning false if absent.
func optionalBool(a map[string]any, name string) (bool, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, invalidArgError(name, "must be a boolean, got "+jsonTypeName(v))
	}
	return b, nil
}

// requiredInt extracts a required positive-integer argument.
func requiredInt(a map[string]any, name string) (int, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return 0, missingArgError(name)
	}
	return strictInt(a, name, v)
}

// optionalInt extracts an optional integer argument, returning 0 if absent.
// JSON numbers arrive as float64; strings must be rejected, not coerced
// ("500" silently becoming 0 was NEW-6 in the architecture review).
func optionalInt(a map[string]any, name string) (int, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return 0, nil
	}
	return strictInt(a, name, v)
}

func strictInt(a map[string]any, name string, v any) (int, error) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case int:
		f = float64(n)
	default:
		return 0, invalidArgError(name, "must be a number, got "+jsonTypeName(v))
	}
	i := int(f)
	if float64(i) != f {
		return 0, invalidArgError(name, "must be a whole number")
	}
	if i < 0 {
		return 0, invalidArgError(name, "must not be negative")
	}
	return i, nil
}

// optionalEnum extracts an optional string argument restricted to the allowed
// values; an out-of-set value is rejected with the allowed list.
func optionalEnum(a map[string]any, name string, allowed ...string) (string, error) {
	s, err := optionalString(a, name)
	if err != nil || s == "" {
		return "", err
	}
	for _, allow := range allowed {
		if s == allow {
			return s, nil
		}
	}
	return "", invalidArgError(name, fmt.Sprintf("invalid value %q (valid: %s)", s, strings.Join(allowed, ", ")))
}

// optionalStringSlice extracts an optional string array argument. A non-array
// value is rejected: silently dropping it would fall through to the "attach
// all account keys" default in create_vm.
func optionalStringSlice(a map[string]any, name string) ([]string, error) {
	v, ok := a[name]
	if !ok || v == nil {
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, invalidArgError(name, "must be an array of strings, got "+jsonTypeName(v))
	}
	result := make([]string, 0, len(arr))
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, invalidArgError(name, fmt.Sprintf("element %d must be a string, got %s", i, jsonTypeName(item)))
		}
		result = append(result, s)
	}
	return result, nil
}

// jsonTypeName names JSON-ish value kinds for type-mismatch messages.
func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case float64, int:
		return "a number"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
