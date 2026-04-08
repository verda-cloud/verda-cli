package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// errCanceled is returned when the user declines confirmation.
var errCanceled = errors.New("canceled")

const (
	markerStart  = "<!-- verda-ai-skills:start -->"
	markerEnd    = "<!-- verda-ai-skills:end -->"
	methodAppend = "append"
)

type installOptions struct {
	agents         []string
	statePath      string
	fetcher        *fetcher
	agentOverrides map[string]*Agent
}

func NewCmdInstall(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &installOptions{}

	cmd := &cobra.Command{
		Use:   "install [agents...]",
		Short: "Install AI agent skills for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Install skill files that teach AI coding agents how to use the
			Verda CLI. Skills are fetched from:
			https://github.com/verda-cloud/verda-ai-skills

			Without arguments, shows an interactive picker to select agents.
			With arguments, installs for the specified agents directly.

			Supported agents are defined in the skills repository manifest
			and may include: claude-code, cursor, windsurf, codex, gemini, copilot.
		`),
		Example: cmdutil.Examples(`
			# Interactive — select agents from a list
			verda skills install

			# Install for Claude Code (default)
			verda skills install claude-code

			# Install for multiple agents
			verda skills install claude-code cursor windsurf

			# Non-interactive (for CI/scripts)
			verda --agent skills install claude-code
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agents = args
			return runInstall(cmd.Context(), f, ioStreams, opts)
		},
	}
	return cmd
}

func runInstall(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *installOptions) error {
	ft := opts.fetcher
	if ft == nil {
		ft = NewFetcher()
	}

	manifest, err := fetchManifestWithSpinner(ctx, f, ft)
	if err != nil {
		return err
	}

	selectedAgents, err := resolveAgents(ctx, f, opts, manifest)
	if err != nil {
		return err
	}
	if len(selectedAgents) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No agents selected.")
		return nil
	}

	if err := confirmInstall(ctx, f, ioStreams, manifest, selectedAgents); err != nil {
		if errors.Is(err, errCanceled) {
			return nil
		}
		return err
	}

	skillFiles, err := downloadSkillFiles(ctx, f, ft, manifest)
	if err != nil {
		return err
	}

	if err := installAndPrint(ioStreams, selectedAgents, manifest, skillFiles); err != nil {
		return err
	}

	if err := saveInstallState(ioStreams, opts, manifest, selectedAgents); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "\nInstalled verda-ai-skills v%s\n", manifest.Version)
	printHints(ioStreams, opts, manifest, selectedAgents)

	return nil
}

func fetchManifestWithSpinner(ctx context.Context, f cmdutil.Factory, ft *fetcher) (*Manifest, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Fetching skills manifest...")
	}
	manifest, err := ft.FetchManifest(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return nil, fmt.Errorf("could not fetch skills: %w\n\n"+
			"Check your internet connection or visit https://github.com/verda-cloud/verda-ai-skills", err)
	}
	return manifest, nil
}

func confirmInstall(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, manifest *Manifest, selectedAgents []*Agent) error {
	if f.AgentMode() {
		return nil
	}
	agentNames := make([]string, len(selectedAgents))
	for i, a := range selectedAgents {
		agentNames[i] = a.DisplayName
	}
	prompt := fmt.Sprintf("Install verda-ai-skills v%s for %s?", manifest.Version, strings.Join(agentNames, ", "))
	confirmed, err := f.Prompter().Confirm(ctx, prompt)
	if err != nil {
		return err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return errCanceled
	}
	return nil
}

func downloadSkillFiles(ctx context.Context, f cmdutil.Factory, ft *fetcher, manifest *Manifest) (map[string]string, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Downloading skill files...")
	}
	skillFiles := make(map[string]string, len(manifest.Skills))
	for _, name := range manifest.Skills {
		content, fetchErr := ft.FetchSkillFile(ctx, name)
		if fetchErr != nil {
			if sp != nil {
				sp.Stop("")
			}
			return nil, fetchErr
		}
		skillFiles[name] = content
	}
	if sp != nil {
		sp.Stop("")
	}
	return skillFiles, nil
}

func installAndPrint(ioStreams cmdutil.IOStreams, selectedAgents []*Agent, manifest *Manifest, skillFiles map[string]string) error {
	for _, agent := range selectedAgents {
		if installErr := installForAgent(agent, skillFiles); installErr != nil {
			return fmt.Errorf("installing for %s: %w", agent.DisplayName, installErr)
		}
		dir := agent.TargetDir()
		if agent.Method == methodAppend {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s: %s\n", agent.DisplayName, filepath.Join(dir, agent.TargetFile()))
		} else {
			for _, name := range manifest.Skills {
				_, _ = fmt.Fprintf(ioStreams.Out, "  %s: %s\n", agent.DisplayName, filepath.Join(dir, name))
			}
		}
	}
	return nil
}

func saveInstallState(ioStreams cmdutil.IOStreams, opts *installOptions, manifest *Manifest, selectedAgents []*Agent) error {
	statePath := opts.statePath
	if statePath == "" {
		var err error
		statePath, err = StatePath()
		if err != nil {
			return err
		}
	}
	state, _ := LoadState(statePath)
	state.Version = manifest.Version
	state.InstalledAt = time.Now()
	for _, a := range selectedAgents {
		if !state.HasAgent(a.Name) {
			state.Agents = append(state.Agents, a.Name)
		}
	}
	if saveErr := SaveState(statePath, state); saveErr != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not save state: %v\n", saveErr)
	}
	return nil
}

func printHints(ioStreams cmdutil.IOStreams, opts *installOptions, manifest *Manifest, selectedAgents []*Agent) {
	statePath := opts.statePath
	if statePath == "" {
		statePath, _ = StatePath()
	}
	state, _ := LoadState(statePath)

	installed := make(map[string]bool, len(selectedAgents))
	for _, a := range selectedAgents {
		installed[a.Name] = true
	}
	var hints []string
	for _, name := range manifest.AgentNames() {
		if !installed[name] && !state.HasAgent(name) {
			hints = append(hints, name)
		}
	}
	if len(hints) > 0 && len(hints) <= 4 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\nAlso using other AI agents? Run:\n")
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  verda skills install %s\n", strings.Join(hints, " "))
	}
}

func resolveAgents(ctx context.Context, f cmdutil.Factory, opts *installOptions, manifest *Manifest) ([]*Agent, error) {
	if len(opts.agents) > 0 {
		resolved := make([]*Agent, 0, len(opts.agents))
		for _, name := range opts.agents {
			a := resolveAgent(opts, manifest, name)
			if a == nil {
				return nil, fmt.Errorf("unknown agent %q. Known agents: %s",
					name, strings.Join(manifest.AgentNames(), ", "))
			}
			resolved = append(resolved, a)
		}
		return resolved, nil
	}

	if f.AgentMode() {
		a := resolveAgent(opts, manifest, defaultAgentName)
		if a == nil {
			return nil, errors.New("claude-code not found in manifest")
		}
		return []*Agent{a}, nil
	}

	labels := manifest.AgentDisplayLabels()
	selected, err := f.Prompter().MultiSelect(ctx, "Select AI agents to install skills for", labels)
	if err != nil {
		return nil, err
	}

	names := manifest.AgentNames()
	resolved := make([]*Agent, 0, len(selected))
	for _, idx := range selected {
		a := resolveAgent(opts, manifest, names[idx])
		if a != nil {
			resolved = append(resolved, a)
		}
	}
	return resolved, nil
}

func resolveAgent(opts *installOptions, manifest *Manifest, name string) *Agent {
	if opts.agentOverrides != nil {
		if a, ok := opts.agentOverrides[name]; ok {
			return a
		}
	}
	return manifest.Agents[name]
}

func installForAgent(agent *Agent, skillFiles map[string]string) error {
	dir := agent.TargetDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if agent.Method == methodAppend {
		return installAppend(agent, dir, skillFiles)
	}
	return installCopy(dir, skillFiles)
}

func installCopy(dir string, skillFiles map[string]string) error {
	for name, content := range skillFiles {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // non-sensitive skill files
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func installAppend(agent *Agent, dir string, skillFiles map[string]string) error {
	path := filepath.Join(dir, agent.TargetFile())

	var block strings.Builder
	block.WriteString(markerStart + "\n")
	for _, content := range skillFiles {
		block.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			block.WriteString("\n")
		}
		block.WriteString("\n")
	}
	block.WriteString(markerEnd)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	content := string(existing)
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx >= 0 && endIdx >= 0 {
		content = content[:startIdx] + block.String() + content[endIdx+len(markerEnd):]
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		content += block.String() + "\n"
	}

	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // non-sensitive skill files
}
