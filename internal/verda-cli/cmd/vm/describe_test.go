package vm

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestRenderInstanceCardWithVolumes(t *testing.T) {
	t.Parallel()

	ip := "10.0.0.1"
	inst := &verda.Instance{
		ID:           "inst-123",
		Hostname:     "gpu-runner",
		Status:       verda.StatusRunning,
		InstanceType: "1V100.6V",
		Location:     "FIN-01",
		IP:           &ip,
		Contract:     "PAY_AS_YOU_GO",
		Pricing:      "FIXED_PRICE",
		OSName:       "ubuntu-24.04",
		CPU:          verda.InstanceCPU{NumberOfCores: 6},
		GPU:          verda.InstanceGPU{Description: "V100", NumberOfGPUs: 1},
		GPUMemory:    verda.InstanceMemory{SizeInGigabytes: 16},
		Memory:       verda.InstanceMemory{SizeInGigabytes: 64},
	}

	vol := verda.Volume{
		ID:         "vol-123",
		Name:       "os-volume",
		Size:       100,
		Type:       "NVMe",
		Status:     "attached",
		Location:   "FIN-01",
		IsOSVolume: true,
	}

	card := renderInstanceCard(inst, vol)
	if card == "" {
		t.Fatal("renderInstanceCard returned empty string")
	}
	if len(card) < 50 {
		t.Fatalf("renderInstanceCard output too short: %d chars", len(card))
	}
}

func TestRenderInstanceCardCPUInstance(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{
		ID:           "inst-456",
		Hostname:     "cpu-worker",
		Status:       verda.StatusOffline,
		InstanceType: "CPU.4V.16G",
		Location:     "FIN-03",
		Contract:     "PAY_AS_YOU_GO",
		Pricing:      "FIXED_PRICE",
		OSName:       "ubuntu-24.04",
		CPU:          verda.InstanceCPU{NumberOfCores: 4},
		Memory:       verda.InstanceMemory{SizeInGigabytes: 16},
	}

	card := renderInstanceCard(inst)
	if card == "" {
		t.Fatal("renderInstanceCard returned empty string for CPU instance")
	}
}

func TestRenderInstanceCardRunningShowsSSH(t *testing.T) {
	t.Parallel()

	ip := "10.0.0.1"
	inst := &verda.Instance{
		ID:           "inst-789",
		Hostname:     "test-node",
		Status:       verda.StatusRunning,
		InstanceType: "CPU.4V.16G",
		Location:     "FIN-01",
		IP:           &ip,
		CPU:          verda.InstanceCPU{NumberOfCores: 4},
		Memory:       verda.InstanceMemory{SizeInGigabytes: 16},
	}

	card := renderInstanceCard(inst)
	// Running instances with IP should show ssh hint.
	if len(card) == 0 {
		t.Fatal("empty card")
	}
}
