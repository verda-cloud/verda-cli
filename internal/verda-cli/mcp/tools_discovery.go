package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func (s *Server) registerDiscoveryTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_locations",
			mcp.WithDescription("List available Verda Cloud datacenter locations"),
		),
		s.handleListLocations,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_instance_types",
			mcp.WithDescription("List available instance types with specs and pricing"),
			mcp.WithBoolean("gpu_only", mcp.Description("Show only GPU instance types")),
			mcp.WithBoolean("cpu_only", mcp.Description("Show only CPU instance types")),
			mcp.WithBoolean("spot", mcp.Description("Show spot pricing")),
		),
		s.handleListInstanceTypes,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("check_availability",
			mcp.WithDescription("Check instance type availability by location"),
			mcp.WithString("location", mcp.Description("Location code, e.g. FIN-01")),
			mcp.WithString("instance_type", mcp.Description("Instance type, e.g. 1V100.6V")),
			mcp.WithBoolean("spot", mcp.Description("Check spot availability")),
		),
		s.handleCheckAvailability,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_images",
			mcp.WithDescription("List available OS images. Optionally filter by instance type or category"),
			mcp.WithString("instance_type", mcp.Description("Filter images compatible with this instance type")),
			mcp.WithString("category", mcp.Description("Filter by category, e.g. ubuntu, pytorch")),
		),
		s.handleListImages,
	)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListLocations(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	locations, err := s.client.Locations.Get(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(locations)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListInstanceTypes(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types, err := s.client.InstanceTypes.Get(ctx, "")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	gpuOnly := optionalBool(args(req), "gpu_only")
	cpuOnly := optionalBool(args(req), "cpu_only")

	if gpuOnly || cpuOnly {
		filtered := types[:0]
		for i := range types {
			t := &types[i]
			isGPU := t.GPU.NumberOfGPUs > 0
			if gpuOnly && isGPU {
				filtered = append(filtered, *t)
			} else if cpuOnly && !isGPU {
				filtered = append(filtered, *t)
			}
		}
		types = filtered
	}

	return jsonResult(types)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleCheckAvailability(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	location := optionalString(args(req), "location")
	instanceType := optionalString(args(req), "instance_type")
	spot := optionalBool(args(req), "spot")

	// If checking a specific instance type, use the targeted API.
	if instanceType != "" {
		available, err := s.client.InstanceAvailability.GetInstanceTypeAvailability(ctx, instanceType, spot, location)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		result := map[string]any{
			"instance_type": instanceType,
			"location":      location,
			"spot":          spot,
			"available":     available,
		}
		return jsonResult(result)
	}

	// Otherwise, return the full availability matrix.
	avail, err := s.client.InstanceAvailability.GetAllAvailabilities(ctx, spot, location)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(avail)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListImages(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	instanceType := optionalString(args(req), "instance_type")

	var images any
	var err error

	if instanceType != "" {
		images, err = s.client.Images.GetImagesByInstanceType(ctx, instanceType)
	} else {
		images, err = s.client.Images.Get(ctx)
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// TODO: filter by category if provided (API doesn't support it natively)
	return jsonResult(images)
}
