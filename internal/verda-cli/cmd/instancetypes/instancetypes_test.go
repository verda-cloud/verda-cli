package instancetypes

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestFilterGPU(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{
		{InstanceType: "1V100.6V", GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
		{InstanceType: "CPU.4V.16G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
		{InstanceType: "4A100.80G", GPU: verda.InstanceGPU{NumberOfGPUs: 4}},
	}

	var filtered []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs > 0 {
			filtered = append(filtered, types[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 GPU types, got %d", len(filtered))
	}
}

func TestFilterCPU(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{
		{InstanceType: "1V100.6V", GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
		{InstanceType: "CPU.4V.16G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
		{InstanceType: "CPU.8V.32G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
	}

	var filtered []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs == 0 {
			filtered = append(filtered, types[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 CPU types, got %d", len(filtered))
	}
}

func TestFilterEmpty(t *testing.T) {
	t.Parallel()

	var types []verda.InstanceTypeInfo
	var filtered []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs > 0 {
			filtered = append(filtered, types[i])
		}
	}

	if len(filtered) != 0 {
		t.Fatalf("expected 0, got %d", len(filtered))
	}
}
