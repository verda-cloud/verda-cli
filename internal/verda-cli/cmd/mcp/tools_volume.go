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
			mcp.WithDescription("Create a new block storage volume (starts billing). REQUIRES confirm: true — show the user the name/size/location first; without it the tool fails with CONFIRMATION_REQUIRED (mirrors --yes in the CLI agent contract)."),
			mcp.WithString("name", mcp.Required(), mcp.Description("Volume name")),
			mcp.WithNumber("size_gb", mcp.Required(), mcp.Description("Volume size in GiB")),
			mcp.WithBoolean("confirm", mcp.Required(), mcp.Description(confirmParamHint)),
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
	a := args(req)

	name, err := requiredString(a, "name")
	if err != nil {
		return toolErrorResult(err), nil
	}
	sizeGB, err := requiredInt(a, "size_gb")
	if err != nil {
		return toolErrorResult(err), nil
	}
	if sizeGB <= 0 {
		return toolErrorResult(invalidArgError("size_gb", "must be a positive integer")), nil
	}
	volType, err := optionalEnum(a, "type", verda.VolumeTypeNVMe, verda.VolumeTypeHDD)
	if err != nil {
		return toolErrorResult(err), nil
	}
	location, err := optionalString(a, "location")
	if err != nil {
		return toolErrorResult(err), nil
	}
	confirm, err := optionalBool(a, "confirm")
	if err != nil {
		return toolErrorResult(err), nil
	}

	// Billing action: explicit confirmation required, mirroring --yes in the
	// CLI agent contract. Gated before any API call.
	if !confirm {
		return toolErrorResult(confirmationRequiredError("create_volume")), nil
	}

	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if volType == "" {
		volType = verda.VolumeTypeNVMe
	}
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
