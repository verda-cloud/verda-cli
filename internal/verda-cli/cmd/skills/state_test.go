package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadState_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state, err := LoadState(filepath.Join(dir, "skills.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Version != "" {
		t.Fatalf("expected empty version, got %q", state.Version)
	}
	if len(state.Agents) != 0 {
		t.Fatalf("expected no agents, got %d", len(state.Agents))
	}
}

func TestSaveAndLoadState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "skills.json")
	now := time.Now().Truncate(time.Second)
	want := &State{
		Version:     "1.0.0",
		InstalledAt: now,
		Agents:      []string{"claude-code", "cursor"},
	}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("save error: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if got.Version != want.Version {
		t.Fatalf("version: expected %q, got %q", want.Version, got.Version)
	}
	if len(got.Agents) != 2 {
		t.Fatalf("agents: expected 2, got %d", len(got.Agents))
	}
	if got.Agents[0] != "claude-code" || got.Agents[1] != "cursor" {
		t.Fatalf("agents: expected [claude-code cursor], got %v", got.Agents)
	}
}

func TestStatePath(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	// Unset VERDA_HOME to ensure we get the default path
	t.Setenv("VERDA_HOME", "")
	path, err := StatePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".verda", "skills.json")
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestStateHasAgent(t *testing.T) {
	t.Parallel()
	s := &State{Agents: []string{"claude-code", "cursor"}}
	if !s.HasAgent("claude-code") {
		t.Fatal("expected HasAgent to return true for claude-code")
	}
	if s.HasAgent("windsurf") {
		t.Fatal("expected HasAgent to return false for windsurf")
	}
}

func TestStateRemoveAgent(t *testing.T) {
	t.Parallel()
	s := &State{Agents: []string{"claude-code", "cursor", "windsurf"}}
	s.RemoveAgent("cursor")
	if len(s.Agents) != 2 {
		t.Fatalf("expected 2 agents after remove, got %d", len(s.Agents))
	}
	if s.HasAgent("cursor") {
		t.Fatal("expected cursor to be removed")
	}
}
