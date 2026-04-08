package skills

import (
	"os"
	"path/filepath"
)

// Agent describes an AI coding agent target for skill installation.
type Agent struct {
	Name        string
	DisplayName string
	Scope       string // "global" or "project"
	TargetDir   func() string
	Method      string // "copy" or "append"
	TargetFile  string // only for "append" method
}

var agents = []Agent{
	{
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Scope:       "global",
		TargetDir: func() string {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, ".claude", "skills")
		},
		Method: "copy",
	},
	{
		Name:        "cursor",
		DisplayName: "Cursor",
		Scope:       "project",
		TargetDir:   func() string { return filepath.Join(".cursor", "rules") },
		Method:      "copy",
	},
	{
		Name:        "windsurf",
		DisplayName: "Windsurf",
		Scope:       "project",
		TargetDir:   func() string { return filepath.Join(".windsurf", "rules") },
		Method:      "copy",
	},
	{
		Name:        "codex",
		DisplayName: "Codex",
		Scope:       "project",
		TargetDir:   func() string { return "." },
		Method:      "append",
		TargetFile:  "AGENTS.md",
	},
	{
		Name:        "gemini",
		DisplayName: "Gemini CLI",
		Scope:       "project",
		TargetDir:   func() string { return "." },
		Method:      "append",
		TargetFile:  "GEMINI.md",
	},
	{
		Name:        "copilot",
		DisplayName: "Copilot",
		Scope:       "project",
		TargetDir:   func() string { return ".github" },
		Method:      "append",
		TargetFile:  "copilot-instructions.md",
	},
}

// AgentByName returns the agent with the given name, or false if not found.
func AgentByName(name string) (Agent, bool) {
	for _, a := range agents {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// AllAgentNames returns the names of all known agents in registration order.
func AllAgentNames() []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}

// AgentDisplayLabels returns human-readable labels for each agent,
// including the target path information.
func AgentDisplayLabels() []string {
	labels := make([]string, len(agents))
	for i, a := range agents {
		dir := a.TargetDir()
		home, _ := os.UserHomeDir()
		display := dir
		if home != "" {
			if rel, err := filepath.Rel(home, dir); err == nil && len(rel) < len(dir) {
				display = "~/" + rel
			}
		}
		if a.Method == "append" {
			display = filepath.Join(display, a.TargetFile)
		} else {
			display += "/"
		}
		labels[i] = a.DisplayName + " (" + display + ")"
	}
	return labels
}
