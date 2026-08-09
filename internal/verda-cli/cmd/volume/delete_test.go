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

// Single-volume delete must also require --yes in agent mode — no silent
// confirmation bypass (the gate must fire without any API client).
func TestDeleteAgentModeSingleRequiresYes(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"volume", "delete", "--id", "vol-123"},
		{"volume", "delete", "vol-123"},
	} {
		var buf bytes.Buffer
		ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
		f := &cmdutil.TestFactory{AgentModeOverride: true}

		root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(NewCmdVolume(f, ioStreams))
		root.SetArgs(args)

		err := root.Execute()
		if err == nil {
			t.Fatalf("%v: expected error: agent mode delete requires --yes", args)
		}
		ae := cmdutil.ClassifyError(err)
		if ae.Code != "CONFIRMATION_REQUIRED" {
			t.Fatalf("%v: code = %q, want CONFIRMATION_REQUIRED (err: %v)", args, ae.Code, err)
		}
		if !strings.Contains(err.Error(), "requires --yes in agent mode") {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
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

// The review sites: usage/flag-misuse errors must classify as VALIDATION_ERROR
// (exit 2) in agent mode — distinct from server-side failures.
func TestDeleteUsageErrorsClassifyAsValidation(t *testing.T) {
	t.Parallel()

	newRoot := func(args ...string) *cobra.Command {
		var buf bytes.Buffer
		ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
		f := &cmdutil.TestFactory{AgentModeOverride: true, OutputFormatOverride: "json"}
		root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(NewCmdVolume(f, ioStreams))
		root.SetArgs(args)
		return root
	}

	cases := map[string][]string{
		"status without all":   {"volume", "delete", "--status", "detached"},
		"all combined with id": {"volume", "delete", "--all", "--id", "vol-1", "--yes"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := newRoot(args...).Execute()
			if err == nil {
				t.Fatal("expected a usage error")
			}
			ae := cmdutil.ClassifyError(err)
			if ae.Code != "VALIDATION_ERROR" {
				t.Fatalf("code = %q, want VALIDATION_ERROR (err: %v)", ae.Code, err)
			}
			if ae.ExitCode != cmdutil.ExitBadArgs {
				t.Fatalf("exit code = %d, want %d", ae.ExitCode, cmdutil.ExitBadArgs)
			}
		})
	}
}
