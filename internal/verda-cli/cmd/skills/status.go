package skills

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type statusOptions struct {
	statePath string
	fetcher   *fetcher
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
			have skills installed, and whether an update is available.
		`),
		Example: cmdutil.Examples(`
			verda skills status
			verda skills status -o json
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

	// Check for updates (best-effort). Also used to resolve agent display names.
	var manifest *Manifest
	if out.Installed {
		ft := opts.fetcher
		if ft == nil {
			ft = NewFetcher()
		}
		if m, fetchErr := ft.FetchManifest(ctx); fetchErr == nil {
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
