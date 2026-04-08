package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentByName(t *testing.T) {
	t.Parallel()
	agent, ok := AgentByName("claude-code")
	if !ok {
		t.Fatal("expected claude-code to be a known agent")
	}
	if agent.DisplayName != "Claude Code" {
		t.Fatalf("expected display name 'Claude Code', got %q", agent.DisplayName)
	}
	if agent.Scope != "global" {
		t.Fatalf("expected scope 'global', got %q", agent.Scope)
	}
}

func TestAgentByNameUnknown(t *testing.T) {
	t.Parallel()
	_, ok := AgentByName("not-real")
	if ok {
		t.Fatal("expected unknown agent to return false")
	}
}

func TestAllAgentNames(t *testing.T) {
	t.Parallel()
	names := AllAgentNames()
	if len(names) < 6 {
		t.Fatalf("expected at least 6 agents, got %d", len(names))
	}
	if names[0] != "claude-code" {
		t.Fatalf("expected first agent to be claude-code, got %q", names[0])
	}
}

func TestAgentTargetDir_ClaudeCode(t *testing.T) {
	t.Parallel()
	agent, _ := AgentByName("claude-code")
	dir := agent.TargetDir()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".claude", "skills")
	if dir != expected {
		t.Fatalf("expected %q, got %q", expected, dir)
	}
}

func TestAgentTargetDir_ProjectScope(t *testing.T) {
	t.Parallel()
	agent, _ := AgentByName("cursor")
	dir := agent.TargetDir()
	expected := filepath.Join(".cursor", "rules")
	if dir != expected {
		t.Fatalf("expected %q, got %q", expected, dir)
	}
}

func TestAgentDisplayLabels(t *testing.T) {
	t.Parallel()
	labels := AgentDisplayLabels()
	if len(labels) < 6 {
		t.Fatalf("expected at least 6 labels, got %d", len(labels))
	}
	// Labels should include path info
	if labels[0] != "Claude Code (~/.claude/skills/)" {
		t.Fatalf("unexpected first label: %q", labels[0])
	}
}
