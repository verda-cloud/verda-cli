package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var defaultSkillNames = []string{"verda-cloud.md", "verda-reference.md"}

type uninstallOptions struct {
	agents         []string
	statePath      string
	skillNames     []string
	agentOverrides map[string]*Agent
}

func NewCmdUninstall(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &uninstallOptions{}
	cmd := &cobra.Command{
		Use:   "uninstall [agents...]",
		Short: "Remove installed AI agent skills",
		Long: cmdutil.LongDesc(`
			Remove verda-ai-skills from the specified AI agents.

			For copy-method agents (Claude Code, Cursor, Windsurf), skill files
			are deleted from the target directory. For append-method agents
			(Codex, Gemini, Copilot), the marked content block is removed from
			the target file, preserving surrounding content.

			Without arguments, shows an interactive picker of currently
			installed agents to select from.
		`),
		Example: cmdutil.Examples(`
			# Interactive — select from installed agents
			verda skills uninstall

			# Remove from specific agent
			verda skills uninstall claude-code

			# Remove from multiple agents
			verda skills uninstall claude-code cursor

			# Non-interactive — remove from all installed agents
			verda --agent skills uninstall
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agents = args
			return runUninstall(cmd.Context(), f, ioStreams, opts)
		},
	}
	return cmd
}

func runUninstall(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *uninstallOptions) error {
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

	if state.Version == "" && len(opts.agents) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No skills installed.")
		return nil
	}

	// Load manifest to resolve agent definitions.
	manifest, _ := fetchManifestForUninstall()

	selectedAgents, err := resolveUninstallAgents(ctx, f, ioStreams, opts, state, manifest)
	if err != nil {
		return err
	}
	if len(selectedAgents) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No agents selected.")
		return nil
	}

	// Confirm.
	if !f.AgentMode() {
		agentNames := make([]string, len(selectedAgents))
		for i, a := range selectedAgents {
			agentNames[i] = a.DisplayName
		}
		prompt := fmt.Sprintf("Remove verda-ai-skills from %s?", strings.Join(agentNames, ", "))
		confirmed, confirmErr := f.Prompter().Confirm(ctx, prompt)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	// Use skill names from state (tracks what was actually installed),
	// fall back to manifest, then hardcoded defaults.
	skillNames := opts.skillNames
	if len(skillNames) == 0 && len(state.Skills) > 0 {
		skillNames = state.Skills
	}
	if len(skillNames) == 0 {
		skillNames = defaultSkillNames
	}

	for _, agent := range selectedAgents {
		if removeErr := uninstallForAgent(agent, skillNames); removeErr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Warning: %s: %v\n", agent.DisplayName, removeErr)
			continue
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  Removed from %s\n", agent.DisplayName)
		state.RemoveAgent(agent.Name)
	}

	if len(state.Agents) == 0 {
		state.Version = ""
	}

	if saveErr := SaveState(statePath, state); saveErr != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not save state: %v\n", saveErr)
	}

	_, _ = fmt.Fprintln(ioStreams.Out, "\nDone.")
	return nil
}

func fetchManifestForUninstall() (*Manifest, error) {
	return LoadManifest()
}

func resolveUninstallAgents(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *uninstallOptions, state *State, manifest *Manifest) ([]*Agent, error) {
	if len(opts.agents) > 0 {
		resolved := make([]*Agent, 0, len(opts.agents))
		for _, name := range opts.agents {
			a := resolveUninstallAgent(opts, manifest, name)
			if a == nil {
				return nil, fmt.Errorf("unknown agent %q", name)
			}
			resolved = append(resolved, a)
		}
		return resolved, nil
	}

	if f.AgentMode() {
		resolved := make([]*Agent, 0, len(state.Agents))
		for _, name := range state.Agents {
			a := resolveUninstallAgent(opts, manifest, name)
			if a != nil {
				resolved = append(resolved, a)
			}
		}
		return resolved, nil
	}

	if len(state.Agents) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No agents with skills installed.")
		return nil, nil
	}

	labels := make([]string, len(state.Agents))
	for i, name := range state.Agents {
		a := resolveUninstallAgent(opts, manifest, name)
		if a != nil {
			labels[i] = a.DisplayName
		} else {
			labels[i] = name
		}
	}

	selected, selectErr := f.Prompter().MultiSelect(ctx, "Select agents to remove skills from", labels)
	if selectErr != nil {
		return nil, selectErr
	}

	resolved := make([]*Agent, 0, len(selected))
	for _, idx := range selected {
		a := resolveUninstallAgent(opts, manifest, state.Agents[idx])
		if a != nil {
			resolved = append(resolved, a)
		}
	}
	return resolved, nil
}

// resolveUninstallAgent looks up an agent by name: overrides first, then manifest.
func resolveUninstallAgent(opts *uninstallOptions, manifest *Manifest, name string) *Agent {
	if opts.agentOverrides != nil {
		if a, ok := opts.agentOverrides[name]; ok {
			return a
		}
	}
	if manifest != nil {
		return manifest.Agents[name]
	}
	return nil
}

func uninstallForAgent(agent *Agent, skillNames []string) error {
	if agent.Method == methodAppend {
		return uninstallAppend(agent)
	}
	return uninstallCopy(agent, skillNames)
}

func uninstallCopy(agent *Agent, skillNames []string) error {
	dir := agent.TargetDir()
	for _, name := range skillNames {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}

func uninstallAppend(agent *Agent) error {
	path := filepath.Join(agent.TargetDir(), agent.TargetFile())
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	content := string(data)
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx < 0 || endIdx < 0 {
		return nil
	}

	before := strings.TrimRight(content[:startIdx], "\n")
	after := strings.TrimLeft(content[endIdx+len(markerEnd):], "\n")

	var result string
	switch {
	case before != "" && after != "":
		result = before + "\n\n" + after + "\n"
	case before != "":
		result = before + "\n"
	case after != "":
		result = after + "\n"
	}

	return os.WriteFile(path, []byte(result), 0o644) //nolint:gosec // non-sensitive markdown file
}
