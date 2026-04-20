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

package volume

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestDeleteRejectsAllWithID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVolume(f, ioStreams))
	root.SetArgs([]string{"volume", "delete", "--all", "--id", "vol-123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --all combined with --id")
	}
}

func TestDeleteRejectsAllWithPositionalArg(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVolume(f, ioStreams))
	root.SetArgs([]string{"volume", "delete", "--all", "vol-123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when --all combined with positional arg")
	}
}

func TestDeleteAgentModeRequiresYes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVolume(f, ioStreams))
	root.SetArgs([]string{"volume", "delete", "--all", "--status", "detached"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: agent mode batch requires --yes")
	}
}

func TestDeleteStatusRequiresAll(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVolume(f, ioStreams))
	root.SetArgs([]string{"volume", "delete", "--status", "detached"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for --status without --all")
	}
	if !strings.Contains(err.Error(), "--status can only be used with --all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteHasExpectedFlags(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	volCmd := NewCmdVolume(f, ioStreams)

	var deleteCmd *cobra.Command
	for _, sub := range volCmd.Commands() {
		if sub.Name() == "delete" {
			deleteCmd = sub
			break
		}
	}
	if deleteCmd == nil {
		t.Fatal("delete subcommand not found")
	}

	for _, flag := range []string{"id", "all", "status", "yes"} {
		if deleteCmd.Flags().Lookup(flag) == nil {
			t.Errorf("delete missing --%s flag", flag)
		}
	}
}

func TestDeleteHasRmAlias(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	volCmd := NewCmdVolume(f, ioStreams)

	var found bool
	for _, sub := range volCmd.Commands() {
		if sub.HasAlias("rm") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'rm' alias for delete command")
	}
}
