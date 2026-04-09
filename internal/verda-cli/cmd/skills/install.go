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
	force          bool
	statePath      string
	agentOverrides map[string]*Agent
}

func NewCmdInstall(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &installOptions{}

	cmd := &cobra.Command{
		Use:   "install [agents...]",
		Short: "Install AI agent skills for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Install skill files that teach AI coding agents how to use the
			Verda CLI. Skills are bundled with the CLI binary and versioned
			with each release.

			Two skill files are installed per agent:
			  - verda-cloud.md: Decision engine (deploy workflow, cost checks, error recovery)
			  - verda-reference.md: Command reference (flags, output fields, parameter sources)

			Install methods vary by agent:
			  - copy: Files placed in agent's rules directory (Claude Code, Cursor, Windsurf)
			  - append: Content injected between markers in a target file (Codex, Gemini, Copilot)

			Without arguments, shows an interactive picker to select agents.
			With arguments, installs for the specified agents directly.
		`),
		Example: cmdutil.Examples(`
			# Interactive — select agents from a list
			verda skills install

			# Install for Claude Code (writes to ~/.claude/skills/)
			verda skills install claude-code

			# Install for multiple agents at once
			verda skills install claude-code cursor windsurf

			# Reinstall / update without confirmation
			verda skills install claude-code --force

			# Non-interactive for CI/scripts (defaults to claude-code)
			verda --agent skills install claude-code
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agents = args
			return runInstall(cmd.Context(), f, ioStreams, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.force, "force", false, "Skip confirmation and reinstall even if already installed")

	return cmd
}

func runInstall(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *installOptions) error {
	manifest, err := loadManifestWithSpinner(ctx, f)
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

	if err := confirmInstall(ctx, f, ioStreams, opts, manifest, selectedAgents); err != nil {
		if errors.Is(err, errCanceled) {
			return nil
		}
		return err
	}

	skillFiles, err := LoadSkillFiles(manifest)
	if err != nil {
		return err
	}

	// Load previous state to detect stale files from renamed skills.
	prevState := loadPreviousState(opts)

	if err := installAndPrint(ioStreams, selectedAgents, manifest, skillFiles, prevState.Skills); err != nil {
		return err
	}

	if err := saveInstallState(ioStreams, opts, manifest, selectedAgents); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "\nInstalled verda-ai-skills v%s\n", manifest.Version)
	printHints(ioStreams, opts, manifest, selectedAgents)

	return nil
}

func loadManifestWithSpinner(ctx context.Context, f cmdutil.Factory) (*Manifest, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading skills manifest...")
	}
	manifest, err := LoadManifest()
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return nil, fmt.Errorf("could not load skills: %w", err)
	}
	return manifest, nil
}

func confirmInstall(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *installOptions, manifest *Manifest, selectedAgents []*Agent) error {
	if f.AgentMode() || opts.force {
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

func loadPreviousState(opts *installOptions) *State {
	statePath := opts.statePath
	if statePath == "" {
		var err error
		statePath, err = StatePath()
		if err != nil {
			return &State{}
		}
	}
	state, _ := LoadState(statePath)
	return state
}

func installAndPrint(ioStreams cmdutil.IOStreams, selectedAgents []*Agent, manifest *Manifest, skillFiles map[string]string, previousSkills []string) error {
	for _, agent := range selectedAgents {
		if installErr := installForAgent(agent, skillFiles, previousSkills); installErr != nil {
			return fmt.Errorf("installing for %s: %w", agent.DisplayName, installErr)
		}
		dir := agent.TargetDir()
		if agent.Method == methodAppend {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s: %s\n", agent.DisplayName, filepath.Join(dir, agent.TargetFile()))
		} else {
			for _, name := range manifest.Skills {
				_, _ = fmt.Fprintf(ioStreams.Out, "  %s: %s\n", agent.DisplayName, filepath.Join(dir, agent.DestName(name)))
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
	state.Skills = manifest.Skills
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

func installForAgent(agent *Agent, skillFiles map[string]string, previousSkills []string) error {
	dir := agent.TargetDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if agent.Method == methodAppend {
		return installAppend(agent, dir, skillFiles)
	}
	// Remove stale files from a previous install (e.g. renamed skills).
	cleanupStaleFiles(dir, agent, skillFiles, previousSkills)
	return installCopy(dir, agent, skillFiles)
}

// cleanupStaleFiles removes previously installed skill files that are no longer
// in the current manifest. This handles file renames across CLI versions.
func cleanupStaleFiles(dir string, agent *Agent, currentFiles map[string]string, previousSkills []string) {
	// Build set of current destination filenames.
	current := make(map[string]bool, len(currentFiles))
	for name := range currentFiles {
		current[agent.DestName(name)] = true
	}
	for _, old := range previousSkills {
		// Apply file_map to resolve destination name (handles both plain
		// names and mapped names like verda-cloud.md → SKILL.md).
		oldDest := agent.DestName(old)
		if current[oldDest] {
			continue // still in current manifest
		}
		_ = os.Remove(filepath.Join(dir, oldDest)) // best-effort
	}
}

func installCopy(dir string, agent *Agent, skillFiles map[string]string) error {
	for name, content := range skillFiles {
		path := filepath.Join(dir, agent.DestName(name))
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

	existing, err := os.ReadFile(filepath.Clean(path))
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
