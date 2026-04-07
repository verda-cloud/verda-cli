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
)

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
			mcp.WithDescription("Create a new Verda Cloud VM instance. Before calling, ensure you have: instance_type, image, hostname, ssh_key_ids (ask user to pick), and os_volume_size_gb. Use vm_availability to check stock and list_images for image options. Always show cost estimate and get user confirmation first."),
			mcp.WithString("instance_type", mcp.Required(), mcp.Description("Instance type, e.g. 1V100.6V or CPU.4V.16G")),
			mcp.WithString("image", mcp.Required(), mcp.Description("OS image slug, e.g. ubuntu-24.04-cuda-12.8-open-docker")),
			mcp.WithString("hostname", mcp.Required(), mcp.Description("Hostname for the new VM")),
			mcp.WithString("location", mcp.Description("Location code. If omitted, automatically picks a location that has stock for the requested instance type.")),
			mcp.WithString("description", mcp.Description("Human-readable description")),
			mcp.WithNumber("os_volume_size_gb", mcp.Required(), mcp.Description("OS volume size in GiB (e.g. 50, 100, 200)")),
			mcp.WithArray("ssh_key_ids", mcp.Required(), mcp.Description("SSH key IDs or names to inject. Names are resolved automatically (e.g. 'meng'). Use list_ssh_keys to find available keys and ask the user which one to use.")),
			mcp.WithString("startup_script_id", mcp.Description("Startup script ID")),
			mcp.WithBoolean("spot", mcp.Description("Request a spot instance")),
			mcp.WithNumber("storage_size_gb", mcp.Description("Additional storage size in GiB")),
			mcp.WithString("storage_type", mcp.Description("Storage type: NVMe or HDD (default NVMe)")),
			mcp.WithBoolean("wait", mcp.Description("Wait for the VM to be ready (default true)")),
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
			mcp.WithDescription("Perform an action on a VM: start, shutdown, force_shutdown, hibernate, or delete. IMPORTANT: Always confirm with the user before destructive actions."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Instance ID")),
			mcp.WithString("action", mcp.Required(), mcp.Description("Action: start, shutdown, force_shutdown, hibernate, delete")),
			mcp.WithBoolean("wait", mcp.Description("Wait for the action to complete (default true)")),
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

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleVMAvailability(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	location := optionalString(args(req), "location")
	instanceType := optionalString(args(req), "instance_type")
	gpuOnly := optionalBool(args(req), "gpu_only")
	cpuOnly := optionalBool(args(req), "cpu_only")
	spot := optionalBool(args(req), "spot")

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
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	status := optionalString(args(req), "status")
	instances, err := client.Instances.Get(ctx, status)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(instances)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleDescribeVM(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	id, err := requiredString(args(req), "id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	inst, err := client.Instances.GetByID(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(inst)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleCreateVM(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	instanceType, err := requiredString(args(req), "instance_type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	image, err := requiredString(args(req), "image")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	hostname, err := requiredString(args(req), "hostname")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	location := optionalString(args(req), "location")
	if location == "" {
		// Auto-pick a location that has stock for this instance type.
		loc, err := s.findAvailableLocation(ctx, client, instanceType, optionalBool(args(req), "spot"))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		location = loc
	}
	description := optionalString(args(req), "description")
	if description == "" {
		description = hostname
	}

	// Resolve SSH key names to IDs, or use the most recent key as default.
	sshKeyInputs := optionalStringSlice(args(req), "ssh_key_ids")
	sshKeyIDs, err := s.resolveSSHKeyIDs(ctx, client, sshKeyInputs)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(sshKeyIDs) == 0 {
		return mcp.NewToolResultError("ssh_key_ids is required. Use list_ssh_keys to find available keys and ask the user which one to use."), nil
	}

	createReq := verda.CreateInstanceRequest{
		InstanceType: instanceType,
		Image:        image,
		Hostname:     hostname,
		Description:  description,
		LocationCode: location,
		SSHKeyIDs:    sshKeyIDs,
		IsSpot:       optionalBool(args(req), "spot"),
	}

	if scriptID := optionalString(args(req), "startup_script_id"); scriptID != "" {
		createReq.StartupScriptID = &scriptID
	}

	if osVolumeSize := optionalInt(args(req), "os_volume_size_gb"); osVolumeSize > 0 {
		createReq.OSVolume = &verda.OSVolumeCreateRequest{
			Name: hostname + "-os",
			Size: osVolumeSize,
		}
	}

	if storageSize := optionalInt(args(req), "storage_size_gb"); storageSize > 0 {
		storageType := optionalString(args(req), "storage_type")
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

	// Wait for VM to be ready if requested (default true).
	wait := true
	if v, ok := args(req)["wait"]; ok {
		if b, ok := v.(bool); ok {
			wait = b
		}
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

	return jsonResult(inst)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleVMAction(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	id, err := requiredString(args(req), "id")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	action, err := requiredString(args(req), "action")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	switch action {
	case "start":
		err = client.Instances.Start(ctx, id)
	case "shutdown":
		err = client.Instances.Shutdown(ctx, id)
	case "force_shutdown":
		err = client.Instances.ForceShutdown(ctx, id)
	case "hibernate":
		err = client.Instances.Hibernate(ctx, id)
	case "delete":
		err = client.Instances.Delete(ctx, []string{id}, nil, false)
	default:
		return mcp.NewToolResultError(fmt.Sprintf("unknown action %q: use start, shutdown, force_shutdown, hibernate, or delete", action)), nil
	}

	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]string{
		"id":     id,
		"action": action,
		"status": "completed",
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
