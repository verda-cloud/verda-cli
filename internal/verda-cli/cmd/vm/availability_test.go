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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestVMAvailabilityAcceptsNoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	vmCmd := &cobra.Command{Use: "vm"}
	vmCmd.AddCommand(NewCmdAvailability(f, ioStreams))
	root.AddCommand(vmCmd)
	root.SetArgs([]string{"vm", "availability"})

	err := root.Execute()
	// Should fail on VerdaClient, NOT on arg validation.
	if err == nil {
		t.Fatal("expected error from VerdaClient")
	}
}

func TestVMAvailabilityRejectsPositionalArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	vmCmd := &cobra.Command{Use: "vm"}
	vmCmd.AddCommand(NewCmdAvailability(f, ioStreams))
	root.AddCommand(vmCmd)
	root.SetArgs([]string{"vm", "availability", "extra-arg"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for positional args")
	}
}

func TestMatchesKindGPU(t *testing.T) {
	t.Parallel()

	if !matchesKind("1V100.6V", "gpu") {
		t.Error("expected 1V100.6V to match gpu")
	}
	if matchesKind("CPU.4V.16G", "gpu") {
		t.Error("expected CPU.4V.16G to NOT match gpu")
	}
}

func TestMatchesKindCPU(t *testing.T) {
	t.Parallel()

	if !matchesKind("CPU.4V.16G", "cpu") {
		t.Error("expected CPU.4V.16G to match cpu")
	}
	if matchesKind("1V100.6V", "cpu") {
		t.Error("expected 1V100.6V to NOT match cpu")
	}
}

func TestMatchesKindEmpty(t *testing.T) {
	t.Parallel()

	if !matchesKind("1V100.6V", "") {
		t.Error("expected empty kind to match anything")
	}
	if !matchesKind("CPU.4V.16G", "") {
		t.Error("expected empty kind to match anything")
	}
}
