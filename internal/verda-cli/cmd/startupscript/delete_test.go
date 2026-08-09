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
