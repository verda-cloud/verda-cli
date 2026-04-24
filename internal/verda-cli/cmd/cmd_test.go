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

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRegistryEnabled_EnvVar(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true}, {"true", true}, {"0", false}, {"", false}, {"yes", false},
	} {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("VERDA_REGISTRY_ENABLED", tc.val)
			if got := registryEnabled(); got != tc.want {
				t.Fatalf("registryEnabled()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkipCredentialResolution_RegistryChildren(t *testing.T) {
	parent := &cobra.Command{Use: "registry"}
	child := &cobra.Command{Use: "configure"}
	parent.AddCommand(child)
	if !skipCredentialResolution(child) {
		t.Error("child of `registry` should skip credential resolution")
	}
}

func TestShouldCheckVersion(t *testing.T) {
	// newCmd returns a *cobra.Command whose Name() is `name` (cobra derives
	// Name from the first token of Use).
	newCmd := func(name string) *cobra.Command { return &cobra.Command{Use: name} }

	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		// ---- Yes: CLI-meta commands the user is already asking about. ----
		{"doctor", newCmd("doctor"), true},
		{"update", newCmd("update"), true},
		{"help", newCmd("help"), true},
		{"verda root (bare)", newCmd("verda"), true},

		// ---- No: resource / business commands. They must NEVER do a
		// network fetch or even read the cache to print a cosmetic hint.
		{"vm", newCmd("vm"), false},
		{"vm list", newCmd("list"), false}, // subcommand runs with its own Name
		{"vccr/registry", newCmd("registry"), false},
		{"s3", newCmd("s3"), false},
		{"volume", newCmd("volume"), false},
		{"template", newCmd("template"), false},
		{"cost", newCmd("cost"), false},
		{"status", newCmd("status"), false},
		{"completion", newCmd("completion"), false},
		{"settings", newCmd("settings"), false},

		// Previously these short-circuited with an early return in PostRun;
		// with the new gate, shouldCheckVersion just returns false for them.
		{"mcp", newCmd("mcp"), false},
		{"skills", newCmd("skills"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldCheckVersion(tc.cmd); got != tc.want {
				t.Errorf("shouldCheckVersion(%q) = %v, want %v", tc.cmd.Name(), got, tc.want)
			}
		})
	}
}
