package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func (s *Server) registerVolumeTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("list_volumes",
			mcp.WithDescription("List all Verda Cloud block storage volumes"),
		),
		s.handleListVolumes,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("create_volume",
			mcp.WithDescription("Create a new block storage volume"),
			mcp.WithString("name", mcp.Required(), mcp.Description("Volume name")),
			mcp.WithNumber("size_gb", mcp.Required(), mcp.Description("Volume size in GiB")),
			mcp.WithString("type", mcp.Description("Volume type: NVMe or HDD (default NVMe)")),
			mcp.WithString("location", mcp.Description("Location code (default FIN-01)")),
		),
		s.handleCreateVolume,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_volumes_in_trash",
			mcp.WithDescription("List volumes that have been moved to trash (recoverable within 96 hours)"),
		),
		s.handleListVolumesInTrash,
	)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListVolumes(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	volumes, err := client.Volumes.ListVolumes(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(volumes)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleCreateVolume(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	name, err := requiredString(args(req), "name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sizeGB := optionalInt(args(req), "size_gb")
	if sizeGB <= 0 {
		return mcp.NewToolResultError("size_gb must be a positive integer"), nil
	}

	volType := optionalString(args(req), "type")
	if volType == "" {
		volType = verda.VolumeTypeNVMe
	}

	location := optionalString(args(req), "location")
	if location == "" {
		location = verda.LocationFIN01
	}

	volID, err := client.Volumes.CreateVolume(ctx, verda.VolumeCreateRequest{
		Name:         name,
		Size:         sizeGB,
		Type:         volType,
		LocationCode: location,
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := map[string]any{
		"id":       volID,
		"name":     name,
		"size_gb":  sizeGB,
		"type":     volType,
		"location": location,
	}
	return jsonResult(result)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleListVolumesInTrash(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	volumes, err := client.Volumes.GetVolumesInTrash(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(volumes)
}
