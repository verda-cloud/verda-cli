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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

// newMockBackedServer returns an MCP Server whose Verda client talks to the
// contract-suite mock API. State is per-test via mockapi.Server.
func newMockBackedServer(t *testing.T) (*Server, *mockapi.Server) {
	t.Helper()
	mock := mockapi.New()
	t.Cleanup(mock.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(mock.URL()),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return NewServer(client), mock
}

func callReq(name string, arguments map[string]any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Name = name
	r.Params.Arguments = arguments
	return r
}

// parseSuccess parses a non-error JSON tool result into a map.
func parseSuccess(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("expected success, got error: %s", resultText(t, res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &out); err != nil {
		t.Fatalf("result is not a JSON object: %v", err)
	}
	return out
}

// --- Confirm gates (review NEW-4): destructive/billing tools require
// confirm: true and report the CLI's CONFIRMATION_REQUIRED contract code.

func TestCreateVolumeConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)

	volArgs := func(confirm any) map[string]any {
		a := map[string]any{"name": "test-vol", "size_gb": float64(100)}
		if confirm != nil {
			a["confirm"] = confirm
		}
		return a
	}

	res, err := s.handleCreateVolume(context.Background(), callReq("create_volume", volArgs(nil)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")
	if mock.VolumeCount() != 0 {
		t.Fatalf("volume created without confirm; mock has %d volumes", mock.VolumeCount())
	}

	res, err = s.handleCreateVolume(context.Background(), callReq("create_volume", volArgs(false)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")

	res, err = s.handleCreateVolume(context.Background(), callReq("create_volume", volArgs(true)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	if out["id"] == "" || out["size_gb"] != float64(100) {
		t.Errorf("unexpected create result: %v", out)
	}
	if mock.VolumeCount() != 1 {
		t.Fatalf("with confirm the volume should exist; mock has %d", mock.VolumeCount())
	}
}

func TestCreateVMConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	key := mock.SeedSSHKey("deploy")

	vmArgs := func(confirm any) map[string]any {
		a := map[string]any{
			"instance_type": mockapi.TypeCPU,
			"image":         "ubuntu-24.04",
			"hostname":      "mcp-test",
			"location":      verda.LocationFIN01,
			"ssh_key_ids":   []any{key.ID},
			"wait":          false,
		}
		if confirm != nil {
			a["confirm"] = confirm
		}
		return a
	}

	res, err := s.handleCreateVM(context.Background(), callReq("create_vm", vmArgs(nil)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")
	// No instance may exist or be billed without confirmation: the only GET
	// /instances/{id} traffic allowed is zero.
	if got := mock.InstanceGetCount(); got != 0 {
		t.Fatalf("unexpected instance polling without confirm: %d GETs", got)
	}

	res, err = s.handleCreateVM(context.Background(), callReq("create_vm", vmArgs(true)))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	inst, ok := out["instance"].(map[string]any)
	if !ok || inst["id"] == "" {
		t.Fatalf("expected created instance, got %v", out)
	}
	if ids, _ := inst["ssh_key_ids"].([]any); len(ids) != 1 || ids[0] != key.ID {
		t.Errorf("ssh_key_ids = %v, want seeded key only", inst["ssh_key_ids"])
	}
}

func TestVMActionConfirmGate(t *testing.T) {
	t.Parallel()

	destructive := []string{"shutdown", "force_shutdown", "hibernate", "delete"}
	for _, action := range destructive {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s, mock := newMockBackedServer(t)
			inst := mock.SeedInstance("gate-test", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)

			res, err := s.handleVMAction(context.Background(), callReq("vm_action", map[string]any{
				"id":     inst.ID,
				"action": action,
			}))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")

			res, err = s.handleVMAction(context.Background(), callReq("vm_action", map[string]any{
				"id":      inst.ID,
				"action":  action,
				"confirm": true,
			}))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			out := parseSuccess(t, res)
			if out["status"] != "accepted" {
				t.Errorf("status = %v, want accepted", out["status"])
			}
		})
	}

	// start is not destructive and must work without confirm.
	s, mock := newMockBackedServer(t)
	inst := mock.SeedInstance("start-test", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
	res, err := s.handleVMAction(context.Background(), callReq("vm_action", map[string]any{
		"id":     inst.ID,
		"action": "start",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out := parseSuccess(t, res); out["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", out["status"])
	}
}

// --- Honest action semantics (review NEW-5): default reports 'accepted';
// wait: true polls to the expected status and reports 'completed', surfacing
// failed transitions as tool errors.

func TestVMActionWaitCompletes(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	inst := mock.SeedInstance("wait-test", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)

	res, err := s.handleVMAction(context.Background(), callReq("vm_action", map[string]any{
		"id":      inst.ID,
		"action":  "shutdown",
		"confirm": true,
		"wait":    true,
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed", out)
	}
	if out["instance_status"] != verda.StatusOffline {
		t.Errorf("instance_status = %v, want offline", out["instance_status"])
	}
	if got := mock.InstanceGetCount(); got == 0 {
		t.Error("wait=true must poll the instance status (0 GET /instances/{id})")
	}
}

func TestVMActionWaitFailedTransitionIsError(t *testing.T) {
	t.Parallel()

	// The action call succeeds but the instance lands in "error" — the tool
	// must surface a tool error, not claim completion.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "t", "token_type": "Bearer"})
	})
	mux.HandleFunc("PUT /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode([]verda.InstanceActionResult{
			{Action: "shutdown", InstanceID: "inst-1", Status: "completed"},
		})
	})
	mux.HandleFunc("GET /instances/inst-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.Instance{ID: "inst-1", Status: verda.StatusError})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	s := NewServer(client)

	res, err := s.handleVMAction(context.Background(), callReq("vm_action", map[string]any{
		"id":      "inst-1",
		"action":  "shutdown",
		"confirm": true,
		"wait":    true,
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("wait on failed transition must be a tool error, got: %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "error") {
		t.Errorf("error should mention the failed status, got: %s", text)
	}
}

// --- Strict argument validation (review NEW-6) ---
// Table-driven: mismatched JSON types must be rejected with VALIDATION_ERROR
// (mcp-go performs no schema validation), enums against their allowed sets,
// missing required arguments with MISSING_REQUIRED_FLAGS.

func TestStrictArgValidation(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	inst := mock.SeedInstance("args-test", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
	key := mock.SeedSSHKey("deploy")

	tests := []struct {
		name string
		call func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		req  map[string]any
		code string
	}{
		{
			name: "create_vm string os_volume_size_gb not coerced to 50GB default",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "os_volume_size_gb": "500"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm fractional os_volume_size_gb",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "os_volume_size_gb": 50.5},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm ssh_key_ids as string must not fan out to all keys",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "ssh_key_ids": key.ID},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm ssh_key_ids with non-string element",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "ssh_key_ids": []any{key.ID, 42.0}},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm unknown storage_type",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "storage_type": "SCSI"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm wait as string",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img", "hostname": "h", "wait": "yes"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_vm missing hostname",
			call: s.handleCreateVM,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "image": "img"},
			code: "MISSING_REQUIRED_FLAGS",
		},
		{
			name: "vm_action unknown action enum",
			call: s.handleVMAction,
			req:  map[string]any{"id": inst.ID, "action": "reboot"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "vm_action confirm as string",
			call: s.handleVMAction,
			req:  map[string]any{"id": inst.ID, "action": "shutdown", "confirm": "true"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "describe_vm id as number",
			call: s.handleDescribeVM,
			req:  map[string]any{"id": 123.0},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_volume size_gb as string (was: coerced to 0 then fail on positivity)",
			call: s.handleCreateVolume,
			req:  map[string]any{"name": "v", "size_gb": "500"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_volume zero size",
			call: s.handleCreateVolume,
			req:  map[string]any{"name": "v", "size_gb": 0.0, "confirm": true},
			code: "VALIDATION_ERROR",
		},
		{
			name: "create_volume unknown type enum",
			call: s.handleCreateVolume,
			req:  map[string]any{"name": "v", "size_gb": 100.0, "type": "SCSI", "confirm": true},
			code: "VALIDATION_ERROR",
		},
		{
			name: "estimate_cost os_volume_gb as string",
			call: s.handleEstimateCost,
			req:  map[string]any{"instance_type": mockapi.TypeCPU, "os_volume_gb": "100"},
			code: "VALIDATION_ERROR",
		},
		{
			name: "list_vms status as number",
			call: s.handleListVMs,
			req:  map[string]any{"status": 1.0},
			code: "VALIDATION_ERROR",
		},
		{
			name: "vm_availability gpu_only as string",
			call: s.handleVMAvailability,
			req:  map[string]any{"gpu_only": "yes"},
			code: "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := tt.call(context.Background(), callReq("test", tt.req))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			assertToolErrorCode(t, res, tt.code)
		})
	}
}

// --- estimate_cost (item 5): unknown storage_type must error, not price $0.

func TestEstimateCostUnknownStorageType(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)

	res, err := s.handleEstimateCost(context.Background(), callReq("estimate_cost", map[string]any{
		"instance_type": mockapi.TypeCPU,
		"storage_gb":    100.0,
		"storage_type":  "HDD_Shared_Whatever",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	assertToolErrorCode(t, res, "VALIDATION_ERROR")
	if text := resultText(t, res); !strings.Contains(text, "HDD") || !strings.Contains(text, "NVMe") {
		t.Errorf("error should list valid volume types, got: %s", text)
	}
}

func TestEstimateCostSuccess(t *testing.T) {
	t.Parallel()
	s, _ := newMockBackedServer(t)

	res, err := s.handleEstimateCost(context.Background(), callReq("estimate_cost", map[string]any{
		"instance_type": mockapi.TypeCPU,
		"os_volume_gb":  100.0,
		"storage_gb":    200.0,
		"storage_type":  "HDD",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := parseSuccess(t, res)
	est, ok := out["estimate"].(map[string]any)
	if !ok {
		t.Fatalf("missing estimate: %v", out)
	}
	breakdown, _ := est["breakdown"].(map[string]any)
	if got := breakdown["instance"]; got != mockapi.CPUOnDemandTotal {
		t.Errorf("instance hourly = %v, want %v (catalog TOTAL)", got, mockapi.CPUOnDemandTotal)
	}
	if osVol, _ := breakdown["os_volume"].(float64); osVol <= 0 {
		t.Errorf("os_volume hourly = %v, want > 0 for 100GB NVMe", osVol)
	}
	if st, _ := breakdown["storage"].(float64); st <= 0 {
		t.Errorf("storage hourly = %v, want > 0 for 200GB HDD", st)
	}
}
