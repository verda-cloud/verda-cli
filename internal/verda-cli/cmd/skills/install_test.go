package skills

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestInstallCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillFiles := map[string]string{
		"verda-cloud.md":     "# Verda Cloud\ntest",
		"verda-reference.md": "# Commands\ntest",
	}
	agent := &Agent{
		Name: "test-agent", DisplayName: "Test Agent",
		Scope: "global", Method: "copy", Target: dir,
	}
	if err := installForAgent(agent, skillFiles, nil); err != nil {
		t.Fatalf("install error: %v", err)
	}
	for name, content := range skillFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != content {
			t.Fatalf("content mismatch for %s", name)
		}
	}
}

func TestInstallAppend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillFiles := map[string]string{"verda-cloud.md": "# Verda Cloud\ntest"}
	agent := &Agent{
		Name: "codex", DisplayName: "Codex",
		Scope: "project", Method: methodAppend,
		Target: filepath.Join(dir, "AGENTS.md"),
	}
	if err := installForAgent(agent, skillFiles, nil); err != nil {
		t.Fatalf("install error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !bytes.Contains(data, []byte(markerStart)) {
		t.Fatal("expected start marker")
	}
	if !bytes.Contains(data, []byte(markerEnd)) {
		t.Fatal("expected end marker")
	}
	if !bytes.Contains(data, []byte("# Verda Cloud")) {
		t.Fatal("expected skill content")
	}
}

func TestInstallAppend_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := &Agent{
		Name: "codex", Scope: "project", Method: methodAppend,
		Target: filepath.Join(dir, "AGENTS.md"),
	}
	_ = installForAgent(agent, map[string]string{"verda-cloud.md": "# V1"}, nil)
	_ = installForAgent(agent, map[string]string{"verda-cloud.md": "# V2"}, nil)

	data, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if bytes.Count(data, []byte(markerStart)) != 1 {
		t.Fatalf("expected exactly 1 start marker, got content:\n%s", data)
	}
	if !bytes.Contains(data, []byte("# V2")) {
		t.Fatal("expected updated content")
	}
}

func TestInstallCopy_CleansUpStaleFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Simulate a previous install with an old filename.
	oldFile := filepath.Join(dir, "verda-commands.md")
	_ = os.WriteFile(oldFile, []byte("old content"), 0o644)

	// Install with new filenames — old file should be cleaned up.
	newFiles := map[string]string{
		"verda-cloud.md":     "# Cloud",
		"verda-reference.md": "# Reference",
	}
	agent := &Agent{
		Name: "test", DisplayName: "Test",
		Scope: "global", Method: "copy", Target: dir,
	}
	previousSkills := []string{"verda-cloud.md", "verda-commands.md"}

	if err := installForAgent(agent, newFiles, previousSkills); err != nil {
		t.Fatalf("install error: %v", err)
	}

	// New files should exist.
	if _, err := os.Stat(filepath.Join(dir, "verda-reference.md")); err != nil {
		t.Fatal("verda-reference.md should exist")
	}
	// Old renamed file should be removed.
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("verda-commands.md should have been removed")
	}
	// File that exists in both old and new should still exist.
	if _, err := os.Stat(filepath.Join(dir, "verda-cloud.md")); err != nil {
		t.Fatal("verda-cloud.md should still exist")
	}
}

func TestRunInstall_NonInteractive(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "skills.json")

	mock := tuitest.New().AddConfirm(true)
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &installOptions{
		agents:    []string{"claude-code"},
		statePath: statePath,
		agentOverrides: map[string]*Agent{
			"claude-code": {
				Name: "claude-code", DisplayName: "Claude Code",
				Scope: "global", Method: "copy", Target: targetDir,
			},
		},
	}

	if err := runInstall(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("install error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "verda-cloud.md")); err != nil {
		t.Fatal("verda-cloud.md not installed")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "verda-reference.md")); err != nil {
		t.Fatal("verda-reference.md not installed")
	}
	state, _ := LoadState(statePath)
	if state.Version == "" {
		t.Fatal("expected non-empty version in state")
	}
	if !state.HasAgent("claude-code") {
		t.Fatal("expected claude-code in state")
	}
}

func TestRunInstall_UserCancels(t *testing.T) {
	t.Parallel()

	mock := tuitest.New().AddConfirm(false)
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &installOptions{
		agents:    []string{"claude-code"},
		statePath: filepath.Join(t.TempDir(), "skills.json"),
	}

	if err := runInstall(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("expected nil on cancel, got: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Canceled")) {
		t.Fatal("expected canceled message")
	}
}
