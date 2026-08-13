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

package startupscript

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Agent mode must refuse delete without --yes (before touching the API).
func TestDeleteAgentModeRequiresYes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdStartupScript(f, ioStreams))
	root.SetArgs([]string{"startup-script", "delete", "--id", "script-123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error: agent mode delete requires --yes")
	}
	ae := cmdutil.ClassifyError(err)
	if ae.Code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q, want CONFIRMATION_REQUIRED (err: %v)", ae.Code, err)
	}
}

func TestDeleteHasYesFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	scriptCmd := NewCmdStartupScript(f, ioStreams)

	var deleteCmd *cobra.Command
	for _, sub := range scriptCmd.Commands() {
		if sub.Name() == "delete" {
			deleteCmd = sub
			break
		}
	}
	if deleteCmd == nil {
		t.Fatal("delete subcommand not found")
	}
	if deleteCmd.Flags().Lookup("yes") == nil {
		t.Error("delete missing --yes flag")
	}
}

// deleteCmdErr runs `startup-script delete` with args and returns the error. No
// client is configured, so a run that gets as far as resolving one returns
// ErrNoClient — which is exactly how we prove argument parsing succeeded.
func deleteCmdErr(t *testing.T, agent bool, args ...string) error {
	t.Helper()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: agent}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdStartupScript(f, ioStreams))
	root.SetArgs(append([]string{"startup-script", "delete"}, args...))
	return root.Execute()
}

// vm delete and volume delete both take an optional positional id; these two
// commands were --id only, so scripts could not use one calling convention.
func TestDeleteAcceptsPositionalID(t *testing.T) {
	t.Parallel()

	err := deleteCmdErr(t, false, "script-123", "--yes")
	if !errors.Is(err, cmdutil.ErrNoClient) {
		t.Fatalf("err = %v, want ErrNoClient (the positional id was rejected before the API)", err)
	}
}

// --id is published; it must keep working exactly as before.
func TestDeleteStillAcceptsIDFlag(t *testing.T) {
	t.Parallel()

	err := deleteCmdErr(t, false, "--id", "script-123", "--yes")
	if !errors.Is(err, cmdutil.ErrNoClient) {
		t.Fatalf("err = %v, want ErrNoClient", err)
	}
}

// Two ids in one invocation is a typo, not an intent — refuse rather than
// silently picking one (vm's shortcut lets the positional win; not copied).
func TestDeleteRejectsPositionalAndFlagTogether(t *testing.T) {
	t.Parallel()

	err := deleteCmdErr(t, false, "script-123", "--id", "script-456", "--yes")
	if err == nil {
		t.Fatal("expected a usage error when both a positional id and --id are given")
	}
	if errors.Is(err, cmdutil.ErrNoClient) {
		t.Fatal("conflicting ids reached the API layer; must fail before that")
	}
	if !strings.Contains(err.Error(), "--id") {
		t.Errorf("error should name the conflicting flag, got: %v", err)
	}
}

// The agent guard must still fire before any API call when the id is positional.
func TestDeleteAgentModePositionalRequiresYes(t *testing.T) {
	t.Parallel()

	err := deleteCmdErr(t, true, "script-123")
	if err == nil {
		t.Fatal("expected error: agent mode delete requires --yes")
	}
	if ae := cmdutil.ClassifyError(err); ae.Code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q, want CONFIRMATION_REQUIRED (err: %v)", ae.Code, err)
	}
}

func TestDeleteRejectsTwoPositionals(t *testing.T) {
	t.Parallel()

	if err := deleteCmdErr(t, false, "script-123", "script-456", "--yes"); err == nil {
		t.Fatal("expected an error for two positional ids")
	}
}
