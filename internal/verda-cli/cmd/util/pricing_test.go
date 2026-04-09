package util

import (
	"math"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestInstanceBillableUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		inst  verda.Instance
		units int
	}{
		{
			name:  "GPU instance 4x",
			inst:  verda.Instance{GPU: verda.InstanceGPU{NumberOfGPUs: 4}},
			units: 4,
		},
		{
			name:  "GPU instance 1x",
			inst:  verda.Instance{GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
			units: 1,
		},
		{
			name:  "CPU instance 8 cores",
			inst:  verda.Instance{CPU: verda.InstanceCPU{NumberOfCores: 8}},
			units: 8,
		},
		{
			name:  "fallback to 1 when no GPU or CPU info",
			inst:  verda.Instance{},
			units: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InstanceBillableUnits(&tt.inst)
			if got != tt.units {
				t.Fatalf("InstanceBillableUnits() = %d, want %d", got, tt.units)
			}
		})
	}
}

func TestInstanceTotalHourlyCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		inst verda.Instance
		want float64
	}{
		{
			name: "GPU: $2.29/GPU * 4 GPUs = $9.16",
			inst: verda.Instance{PricePerHour: 2.29, GPU: verda.InstanceGPU{NumberOfGPUs: 4}},
			want: 9.16,
		},
		{
			name: "CPU: $0.006975/vCPU * 8 vCPUs",
			inst: verda.Instance{PricePerHour: 0.006975, CPU: verda.InstanceCPU{NumberOfCores: 8}},
			want: 0.0558,
		},
		{
			name: "single GPU",
			inst: verda.Instance{PricePerHour: 2.29, GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
			want: 2.29,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InstanceTotalHourlyCost(&tt.inst)
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("InstanceTotalHourlyCost() = %f, want %f", got, tt.want)
			}
		})
	}
}
