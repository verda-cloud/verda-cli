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
	"fmt"
	"math"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func (s *Server) registerCostTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("get_balance",
			mcp.WithDescription("Check Verda Cloud account balance"),
		),
		s.handleGetBalance,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("estimate_cost",
			mcp.WithDescription("Estimate hourly, daily, and monthly costs for an instance configuration"),
			mcp.WithString("instance_type", mcp.Required(), mcp.Description("Instance type, e.g. 1V100.6V")),
			mcp.WithNumber("os_volume_gb", mcp.Description("OS volume size in GiB")),
			mcp.WithNumber("storage_gb", mcp.Description("Additional storage size in GiB")),
			mcp.WithString("storage_type", mcp.Description("Storage type: NVMe or HDD (default NVMe)")),
			mcp.WithBoolean("spot", mcp.Description("Use spot pricing")),
			mcp.WithString("location", mcp.Description("Location code for pricing")),
		),
		s.handleEstimateCost,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_running_costs",
			mcp.WithDescription("Show costs of currently running instances"),
		),
		s.handleGetRunningCosts,
	)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleGetBalance(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	balance, err := client.Balance.Get(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return jsonResult(balance)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleEstimateCost(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	instanceType, err := requiredString(args(req), "instance_type")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	spot := optionalBool(args(req), "spot")
	osVolumeGB := optionalInt(args(req), "os_volume_gb")
	storageGB := optionalInt(args(req), "storage_gb")
	storageType := optionalString(args(req), "storage_type")
	if storageType == "" {
		storageType = "NVMe"
	}

	// Get instance type pricing by fetching all types and filtering.
	allTypes, err := client.InstanceTypes.Get(ctx, "")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var info *verda.InstanceTypeInfo
	for i := range allTypes {
		if allTypes[i].InstanceType == instanceType {
			info = &allTypes[i]
			break
		}
	}
	if info == nil {
		return mcp.NewToolResultError(fmt.Sprintf("instance type %q not found", instanceType)), nil
	}

	instanceHourly := info.PricePerHour.Float64()
	if spot {
		instanceHourly = info.SpotPrice.Float64()
	}

	// Get volume pricing.
	var osVolumeHourly, storageHourly float64
	if osVolumeGB > 0 || storageGB > 0 {
		volTypes, err := client.VolumeTypes.GetAllVolumeTypes(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		for _, vt := range volTypes {
			monthlyPerGB := vt.Price.PricePerMonthPerGB
			hourlyPerGB := math.Ceil(monthlyPerGB/30/24*10000) / 10000
			if vt.Type == "NVMe" && osVolumeGB > 0 {
				osVolumeHourly = hourlyPerGB * float64(osVolumeGB)
			}
			if vt.Type == storageType && storageGB > 0 {
				storageHourly = hourlyPerGB * float64(storageGB)
			}
		}
	}

	totalHourly := instanceHourly + osVolumeHourly + storageHourly
	result := map[string]any{
		"instance_type": instanceType,
		"spot":          spot,
		"estimate": map[string]any{
			"hourly":  round4(totalHourly),
			"daily":   round4(totalHourly * 24),
			"monthly": round4(totalHourly * 24 * 30),
			"breakdown": map[string]any{
				"instance":  round4(instanceHourly),
				"os_volume": round4(osVolumeHourly),
				"storage":   round4(storageHourly),
			},
		},
	}
	return jsonResult(result)
}

//nolint:gocritic // hugeParam: handler signature defined by mcp-go.
func (s *Server) handleGetRunningCosts(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	client, err := s.verdaClient()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	instances, err := client.Instances.Get(ctx, "running")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	type instanceCost struct {
		ID           string  `json:"id"`
		Hostname     string  `json:"hostname"`
		InstanceType string  `json:"instance_type"`
		HourlyCost   float64 `json:"hourly_cost"`
	}

	var totalHourly float64
	costs := make([]instanceCost, 0, len(instances))
	for i := range instances {
		hourly := instances[i].PricePerHour.Float64()
		totalHourly += hourly
		costs = append(costs, instanceCost{
			ID:           instances[i].ID,
			Hostname:     instances[i].Hostname,
			InstanceType: instances[i].InstanceType,
			HourlyCost:   hourly,
		})
	}

	result := map[string]any{
		"instances":    costs,
		"total_hourly": round4(totalHourly),
	}
	return jsonResult(result)
}

func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}
