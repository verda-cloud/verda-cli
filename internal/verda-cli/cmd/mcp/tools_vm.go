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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const confirmParamHint = "REQUIRED for any action that creates billed or irreversible changes: set true to confirm, after showing the user the exact target and cost. Without it the tool fails with CONFIRMATION_REQUIRED (mirrors --yes in the CLI agent contract)."

func (s *Server) registerVMTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_vms",
			mcp.WithDescription("List Verda Cloud VM instances. Optionally filter by status."),
			mcp.WithString("status", mcp.Description("Filter by status: running, offline, provisioning, etc.")),
		),
		s.handleListVMs,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("describe_vm",
			mcp.WithDescription("Get detailed information about a single VM instance"),
			mcp.WithString("id", mcp.Required(), mcp.Description("Instance ID")),
		),
		s.handleDescribeVM,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_vm",
			mcp.WithDescription("Create a new Verda Cloud VM instance (starts billing). REQUIRES confirm: true — estimate costs first (estimate_cost), show the user the type/location/price, and only then call with confirm: true; without it the tool fails with CONFIRMATION_REQUIRED. Required: instance_type, image, hostname, confirm. Optional: ssh_key_ids (if omitted, all account keys are attached), os_volume_size_gb (default 50), location (auto-picked if omitted). Use vm_availability to check stock and list_images for image options."),
			mcp.WithString("instance_type", mcp.Required(), mcp.Description("Instance type, e.g. 1V100.6V or CPU.4V.16G")),
			mcp.WithString("image", mcp.Required(), mcp.Description("OS image slug, e.g. ubuntu-24.04-cuda-12.8-open-docker")),
			mcp.WithString("hostname", mcp.Required(), mcp.Description("Hostname for the new VM")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
			mcp.WithString("location", mcp.Description("Location code. If omitted, automatically picks a location that has stock for the requested instance type.")),
			mcp.WithString("description", mcp.Description("Human-readable description")),
			mcp.WithNumber("os_volume_size_gb", mcp.Description("OS volume size in GiB (default 50)")),
			mcp.WithArray("ssh_key_ids", mcp.Description("SSH key IDs or names. Names are resolved automatically (e.g. 'meng'). If omitted, all account SSH keys are attached.")),
			mcp.WithString("startup_script_id", mcp.Description("Startup script ID")),
			mcp.WithBoolean("spot", mcp.Description("Request a spot instance")),
			mcp.WithNumber("storage_size_gb", mcp.Description("Additional storage size in GiB")),
			mcp.WithString("storage_type", mcp.Description("Storage type: NVMe or HDD (default NVMe)")),
			mcp.WithBoolean("wait", mcp.Description("Wait for the VM to be in 'running' status (default true)")),
		),
		s.handleCreateVM,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("vm_availability",
			mcp.WithDescription("Show available instance types with specs and pricing per location. Returns only instance types that are currently in stock. This is the best tool to answer 'what can I deploy?' — it combines availability, specs, and pricing in one call."),
			mcp.WithString("location", mcp.Description("Filter by location code, e.g. FIN-01")),
			mcp.WithString("instance_type", mcp.Description("Filter by specific instance type, e.g. 1A6000.10V")),
			mcp.WithBoolean("gpu_only", mcp.Description("Show only GPU instance types")),
			mcp.WithBoolean("cpu_only", mcp.Description("Show only CPU instance types")),
			mcp.WithBoolean("spot", mcp.Description("Show spot pricing and availability")),
		),
		s.handleVMAvailability,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("vm_action",
			mcp.WithDescription("Perform an action on a VM: start, shutdown, force_shutdown, hibernate, or delete. Destructive actions (shutdown, force_shutdown, hibernate, delete) REQUIRE confirm: true — confirm with the user first; without it the tool fails with CONFIRMATION_REQUIRED. Returns status 'accepted' once the API has accepted the action; pass wait: true to poll until the instance reaches its expected status and report status 'completed' (a failed transition, e.g. the instance entering 'error', is a tool error). delete is not polled and always returns 'accepted'."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Instance ID")),
			mcp.WithString("action", mcp.Required(), mcp.Description("Action: start, shutdown, force_shutdown, hibernate, delete")),
			mcp.WithBoolean("confirm", mcp.Description("Set true to confirm destructive actions (shutdown, force_shutdown, hibernate, delete). Not needed for start.")),
			mcp.WithBoolean("wait", mcp.Description("Poll until the instance reaches the action's expected status (default false: return 'accepted' immediately)")),
		),
		s.handleVMAction,
	)
}

type availableInstance struct {
	Location     string  `json:"location"`
	InstanceType string  `json:"instance_type"`
	GPU          string  `json:"gpu"`
	VRAM         string  `json:"vram,omitempty"`
	RAM          string  `json:"ram"`
	CPUCores     int     `json:"cpu_cores"`
	PricePerHour float64 `json:"price_per_hour"`
	SpotPrice    float64 `json:"spot_price,omitempty"`
}

//nolint:gocritic,gocyclo // hugeParam + complexity from strict per-argument type checks.
func (s *Server) handleVMAvailability(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := args(req)
	location, err := optionalString(a, "location")
	if err != nil {
		return toolErrorResult(err), nil
	}
	instanceType, err := optionalString(a, "instance_type")
	if err != nil {
		return toolErrorResult(err), nil
	}
	gpuOnly, err := optionalBool(a, "gpu_only")
	if err != nil {
		return toolErrorResult(err), nil
	}
	cpuOnly, err := optionalBool(a, "cpu_only")
	if err != nil {
		return toolErrorResult(err), nil
	}
	spot, err := optionalBool(a, "spot")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Fetch instance types with pricing.
	types, err := client.InstanceTypes.Get(ctx, "usd")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Fetch availability per location.
	avail, err := client.InstanceAvailability.GetAllAvailabilities(ctx, spot, location)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Index instance types by name.
	typeMap := make(map[string]*verda.InstanceTypeInfo, len(types))
	for i := range types {
		typeMap[types[i].InstanceType] = &types[i]
	}

	// Build joined rows: one per (location, instance type) pair.
	var rows []availableInstance
	for _, la := range avail {
		for _, instType := range la.Availabilities {
			t, ok := typeMap[instType]
			if !ok {
				continue
			}
			if instanceType != "" && !strings.EqualFold(instType, instanceType) {
				continue
			}
			isGPU := t.GPU.NumberOfGPUs > 0
			if gpuOnly && !isGPU {
				continue
			}
			if cpuOnly && isGPU {
				continue
			}

			gpu := "—"
			vram := ""
			if isGPU {
				gpu = fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, t.GPU.Description)
				vram = fmt.Sprintf("%dGB", t.GPUMemory.SizeInGigabytes)
			}

			rows = append(rows, availableInstance{
				Location:     la.LocationCode,
				InstanceType: instType,
				GPU:          gpu,
				VRAM:         vram,
				RAM:          fmt.Sprintf("%dGB", t.Memory.SizeInGigabytes),
				CPUCores:     t.CPU.NumberOfCores,
				PricePerHour: float64(t.PricePerHour),
				SpotPrice:    float64(t.SpotPrice),
			})
		}
	}

	// Sort by price ascending.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PricePerHour != rows[j].PricePerHour {
			return rows[i].PricePerHour < rows[j].PricePerHour
		}
		return rows[i].Location < rows[j].Location
	})

	if len(rows) == 0 {
		return mcp.NewToolResultText("No available instances found matching the criteria."), nil
	}

	return jsonResult(rows)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListVMs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	status, err := optionalString(args(req), "status")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	instances, err := client.Instances.Get(ctx, status)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(instances)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleDescribeVM(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := requiredString(args(req), "id")
	if err != nil {
		return toolErrorResult(err), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	inst, err := client.Instances.GetByID(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(inst)
}

//nolint:gocritic,gocyclo // hugeParam + complexity from auto-resolving location/SSH keys.
func (s *Server) handleCreateVM(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := args(req)

	instanceType, err := requiredString(a, "instance_type")
	if err != nil {
		return toolErrorResult(err), nil
	}
	image, err := requiredString(a, "image")
	if err != nil {
		return toolErrorResult(err), nil
	}
	hostname, err := requiredString(a, "hostname")
	if err != nil {
		return toolErrorResult(err), nil
	}
	location, err := optionalString(a, "location")
	if err != nil {
		return toolErrorResult(err), nil
	}
	description, err := optionalString(a, "description")
	if err != nil {
		return toolErrorResult(err), nil
	}
	scriptID, err := optionalString(a, "startup_script_id")
	if err != nil {
		return toolErrorResult(err), nil
	}
	osVolumeSize, err := optionalInt(a, "os_volume_size_gb")
	if err != nil {
		return toolErrorResult(err), nil
	}
	storageSize, err := optionalInt(a, "storage_size_gb")
	if err != nil {
		return toolErrorResult(err), nil
	}
	storageType, err := optionalEnum(a, "storage_type", verda.VolumeTypeNVMe, verda.VolumeTypeHDD)
	if err != nil {
		return toolErrorResult(err), nil
	}
	spot, err := optionalBool(a, "spot")
	if err != nil {
		return toolErrorResult(err), nil
	}
	wait, err := optionalBool(a, "wait")
	if err != nil {
		return toolErrorResult(err), nil
	}
	if _, present := a["wait"]; !present {
		wait = true
	}
	confirm, err := optionalBool(a, "confirm")
	if err != nil {
		return toolErrorResult(err), nil
	}
	sshKeyInputs, err := optionalStringSlice(a, "ssh_key_ids")
	if err != nil {
		return toolErrorResult(err), nil
	}

	// Billing action: explicit confirmation required, mirroring --yes in the
	// CLI agent contract. Gated before any API call.
	if !confirm {
		return toolErrorResult(confirmationRequiredError("create_vm")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if location == "" {
		// Auto-pick a location that has stock for this instance type.
		loc, err := s.findAvailableLocation(ctx, client, instanceType, spot)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		location = loc
	}
	if description == "" {
		description = hostname
	}

	// Resolve SSH key names to IDs, or use the most recent key as default.
	sshKeyIDs, err := s.resolveSSHKeyIDs(ctx, client, sshKeyInputs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var sshKeysNote string
	if len(sshKeyIDs) == 0 {
		// Attach all account SSH keys as default.
		keys, kerr := client.SSHKeys.GetAllSSHKeys(ctx)
		if kerr == nil && len(keys) > 0 {
			names := make([]string, 0, len(keys))
			for i := range keys {
				sshKeyIDs = append(sshKeyIDs, keys[i].ID)
				names = append(names, keys[i].Name)
			}
			sshKeysNote = fmt.Sprintf("No SSH key specified — attached all %d account keys: %s", len(keys), strings.Join(names, ", "))
		}
	}

	createReq := verda.CreateInstanceRequest{
		InstanceType: instanceType,
		Image:        image,
		Hostname:     hostname,
		Description:  description,
		LocationCode: location,
		SSHKeyIDs:    sshKeyIDs,
		IsSpot:       spot,
	}

	if scriptID != "" {
		createReq.StartupScriptID = &scriptID
	}

	if osVolumeSize == 0 {
		osVolumeSize = 50
	}
	createReq.OSVolume = &verda.OSVolumeCreateRequest{
		Name: hostname + "-os",
		Size: osVolumeSize,
	}

	if storageSize > 0 {
		if storageType == "" {
			storageType = verda.VolumeTypeNVMe
		}
		createReq.Volumes = []verda.VolumeCreateRequest{
			{
				Name:         hostname + "-storage",
				Size:         storageSize,
				Type:         storageType,
				LocationCode: location,
			},
		}
	}

	if createReq.IsSpot {
		createReq.Contract = "SPOT"
	}

	inst, err := client.Instances.Create(ctx, createReq)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if wait {
		inst, err = s.pollInstance(ctx, inst.ID, verda.StatusRunning, 5*time.Minute)
		if err != nil {
			// Return what we have even if polling fails.
			return jsonResult(map[string]any{
				"instance":       inst,
				"poll_error":     err.Error(),
				"poll_timed_out": true,
			})
		}
	}

	result := map[string]any{"instance": inst}
	if sshKeysNote != "" {
		result["note"] = sshKeysNote
	}
	return jsonResult(result)
}

// vmAction describes a supported vm_action operation.
type vmAction struct {
	expectStatus string // polled target when wait=true; empty = not polled
	destructive  bool   // requires confirm=true
	exec         func(ctx context.Context, client *verda.Client, id string) error
}

// vmActions mirrors the CLI's vm action table (cmd/vm/action.go): ExpectStatus
// and the destructive set (shutdown/force_shutdown/hibernate carry warnings
// there; delete is special-cased) must stay in sync.
var vmActions = map[string]vmAction{
	verda.ActionStart: {
		expectStatus: verda.StatusRunning,
		exec:         func(ctx context.Context, c *verda.Client, id string) error { return c.Instances.Start(ctx, id) },
	},
	verda.ActionShutdown: {
		expectStatus: verda.StatusOffline,
		destructive:  true,
		exec:         func(ctx context.Context, c *verda.Client, id string) error { return c.Instances.Shutdown(ctx, id) },
	},
	verda.ActionForceShutdown: {
		expectStatus: verda.StatusOffline,
		destructive:  true,
		exec:         func(ctx context.Context, c *verda.Client, id string) error { return c.Instances.ForceShutdown(ctx, id) },
	},
	verda.ActionHibernate: {
		expectStatus: verda.StatusOffline,
		destructive:  true,
		exec:         func(ctx context.Context, c *verda.Client, id string) error { return c.Instances.Hibernate(ctx, id) },
	},
	verda.ActionDelete: {
		destructive: true,
		exec: func(ctx context.Context, c *verda.Client, id string) error {
			return c.Instances.Delete(ctx, []string{id}, nil, false)
		},
	},
}

// vmActionNames returns the sorted action names for error messages.
func vmActionNames() string {
	names := make([]string, 0, len(vmActions))
	for name := range vmActions {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleVMAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a := args(req)

	id, err := requiredString(a, "id")
	if err != nil {
		return toolErrorResult(err), nil
	}
	actionName, err := requiredString(a, "action")
	if err != nil {
		return toolErrorResult(err), nil
	}
	action, ok := vmActions[actionName]
	if !ok {
		return toolErrorResult(invalidArgError("action", fmt.Sprintf("invalid value %q (valid: %s)", actionName, vmActionNames()))), nil
	}
	wait, err := optionalBool(a, "wait")
	if err != nil {
		return toolErrorResult(err), nil
	}
	confirm, err := optionalBool(a, "confirm")
	if err != nil {
		return toolErrorResult(err), nil
	}

	if action.destructive && !confirm {
		return toolErrorResult(confirmationRequiredError(actionName)), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := action.exec(ctx, client, id); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Truthful default: the API accepted the action; nothing has completed yet.
	result := map[string]any{"id": id, "action": actionName, "status": "accepted"}
	if wait && action.expectStatus != "" {
		inst, err := cmdutil.PollInstanceStatus(ctx, nil, client, id,
			cmdutil.WaitOptions{Wait: true, Timeout: 5 * time.Minute}, action.expectStatus)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("action %q was accepted but the wait failed: %v", actionName, err)), nil
		}
		result["status"] = "completed"
		result["instance_status"] = inst.Status
	}
	return jsonResult(result)
}

// findAvailableLocation finds a location that has stock for the given instance type.
func (s *Server) findAvailableLocation(ctx context.Context, client *verda.Client, instanceType string, spot bool) (string, error) {
	avail, err := client.InstanceAvailability.GetAllAvailabilities(ctx, spot, "")
	if err != nil {
		return "", fmt.Errorf("checking availability: %w", err)
	}
	for _, la := range avail {
		for _, t := range la.Availabilities {
			if strings.EqualFold(t, instanceType) {
				return la.LocationCode, nil
			}
		}
	}
	return "", fmt.Errorf("instance type %q is not available in any location", instanceType)
}

// pollInstance polls until the instance reaches the expected status or timeout.
func (s *Server) pollInstance(ctx context.Context, id, expectStatus string, timeout time.Duration) (*verda.Instance, error) {
	client, err := s.verdaClient()
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	for {
		inst, err := client.Instances.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if inst.Status == expectStatus {
			return inst, nil
		}
		if inst.Status == verda.StatusError {
			return inst, errors.New("instance entered error state")
		}
		if time.Now().After(deadline) {
			return inst, fmt.Errorf("timeout waiting for instance %s to reach %s (current: %s)", id, expectStatus, inst.Status)
		}
		select {
		case <-ctx.Done():
			return inst, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}
