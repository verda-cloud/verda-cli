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

package skills

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type statusOptions struct {
	statePath string
}

type statusOutput struct {
	Installed       bool     `json:"installed"`
	Version         string   `json:"version,omitempty"`
	Latest          string   `json:"latest,omitempty"`
	Agents          []string `json:"agents,omitempty"`
	UpdateAvailable bool     `json:"update_available,omitempty"`
}

func NewCmdStatus(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &statusOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show installed skills status",
		Long: cmdutil.LongDesc(`
			Display the currently installed skills version, which agents
			have skills installed, and whether a newer version is bundled
			with this CLI build.

			The "latest" version is the one embedded in the current CLI binary.
			If your installed version differs, run "verda skills install" to update.
		`),
		Example: cmdutil.Examples(`
			# Human-readable output
			verda skills status

			# Structured output for scripts
			verda skills status -o json

			# Check if update is available (JSON fields: version, latest, update_available)
			verda --agent skills status -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), f, ioStreams, opts)
		},
	}
	return cmd
}

func runStatus(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *statusOptions) error {
	statePath := opts.statePath
	if statePath == "" {
		var err error
		statePath, err = StatePath()
		if err != nil {
			return err
		}
	}

	state, err := LoadState(statePath)
	if err != nil {
		return fmt.Errorf("reading state: %w", err)
	}

	out := statusOutput{
		Installed: state.Version != "",
		Version:   state.Version,
		Agents:    state.Agents,
	}

	// Load manifest to resolve agent display names and check version.
	var manifest *Manifest
	if out.Installed {
		if m, loadErr := LoadManifest(); loadErr == nil {
			manifest = m
			out.Latest = m.Version
			out.UpdateAvailable = m.Version != state.Version
		}
	}

	// Structured output (json/yaml).
	if wrote, writeErr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), out); wrote {
		return writeErr
	}

	// Table output.
	if !out.Installed {
		_, _ = fmt.Fprintln(ioStreams.Out, "Verda AI skills: not installed")
		_, _ = fmt.Fprintln(ioStreams.Out, "\nRun 'verda skills install' to get started.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  Verda AI Skills\n\n")
	_, _ = fmt.Fprintf(ioStreams.Out, "  Version:    %s\n", out.Version)
	if out.Latest != "" {
		_, _ = fmt.Fprintf(ioStreams.Out, "  Latest:     %s\n", out.Latest)
	}
	if out.UpdateAvailable {
		_, _ = fmt.Fprintf(ioStreams.Out, "  Update:     available (run 'verda skills install')\n")
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "  Installed:  %s\n", state.InstalledAt.Format("2006-01-02 15:04"))
	_, _ = fmt.Fprintf(ioStreams.Out, "\n  Agents:\n")
	for _, name := range out.Agents {
		displayName := name
		if manifest != nil {
			if a, ok := manifest.Agents[name]; ok {
				displayName = a.DisplayName
			}
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s\n", displayName)
	}

	return nil
}
