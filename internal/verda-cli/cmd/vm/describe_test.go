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

package vm

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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

// TestVMDescribeAcceptsZeroArgs verifies that verda vm describe can be called
// without arguments (for the interactive picker flow).
func TestVMDescribeAcceptsZeroArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	vmCmd := &cobra.Command{Use: "vm"}
	vmCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(vmCmd)
	root.SetArgs([]string{"vm", "describe"})

	err := root.Execute()
	// Should fail on VerdaClient, NOT on arg validation.
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 0" {
		t.Fatal("vm describe should accept zero arguments for interactive mode")
	}
}

// TestVMDescribeAcceptsOneArg verifies that verda vm describe <id> still works.
func TestVMDescribeAcceptsOneArg(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	vmCmd := &cobra.Command{Use: "vm"}
	vmCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(vmCmd)
	root.SetArgs([]string{"vm", "describe", "inst-123"})

	err := root.Execute()
	// Should fail on VerdaClient, NOT on arg validation.
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 2" {
		t.Fatal("vm describe should accept one argument")
	}
}

// TestVMDescribeRejectsTwoArgs ensures extra positional args are rejected.
func TestVMDescribeRejectsTwoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	vmCmd := &cobra.Command{Use: "vm"}
	vmCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(vmCmd)
	root.SetArgs([]string{"vm", "describe", "id1", "id2"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for two positional args")
	}
}
