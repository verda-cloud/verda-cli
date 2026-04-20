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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

func TestUninstallCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "verda-cloud.md"), []byte("test"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "verda-reference.md"), []byte("test"), 0o600)
	agent := &Agent{
		Name: "test-agent", Scope: "global", Method: "copy",
		Target: dir,
	}
	if err := uninstallForAgent(agent, []string{"verda-cloud.md", "verda-reference.md"}); err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "verda-cloud.md")); !os.IsNotExist(err) {
		t.Fatal("expected verda-cloud.md to be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "verda-reference.md")); !os.IsNotExist(err) {
		t.Fatal("expected verda-reference.md to be deleted")
	}
}

func TestUninstallCopy_SubdirCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create subdirectory-based skill files.
	for _, sub := range []string{"verda-cloud", "verda-reference"} {
		subDir := filepath.Join(dir, sub)
		_ = os.MkdirAll(subDir, 0o750)
		_ = os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("test"), 0o600)
	}
	agent := &Agent{
		Name: "claude-code", Scope: "global", Method: "copy",
		Target: dir,
		FileMap: map[string]string{
			"verda-cloud.md":     "verda-cloud/SKILL.md",
			"verda-reference.md": "verda-reference/SKILL.md",
		},
	}
	if err := uninstallForAgent(agent, []string{"verda-cloud.md", "verda-reference.md"}); err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	// SKILL.md files should be removed.
	for _, sub := range []string{"verda-cloud", "verda-reference"} {
		if _, err := os.Stat(filepath.Join(dir, sub, "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("expected %s/SKILL.md to be deleted", sub)
		}
	}
	// Empty subdirectories should be removed.
	for _, sub := range []string{"verda-cloud", "verda-reference"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); !os.IsNotExist(err) {
			t.Fatalf("expected empty dir %s to be removed", sub)
		}
	}
}

func TestUninstallAppend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := "# My Agents\n\nSome stuff\n\n" + markerStart + "\nskill content\n" + markerEnd + "\n\nMore stuff\n"
	_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(content), 0o600)
	agent := &Agent{
		Name: "codex", Scope: "project", Method: methodAppend,
		Target: filepath.Join(dir, "AGENTS.md"),
	}
	if err := uninstallForAgent(agent, nil); err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Clean(filepath.Join(dir, "AGENTS.md")))
	if bytes.Contains(data, []byte(markerStart)) {
		t.Fatal("expected markers to be removed")
	}
	if !bytes.Contains(data, []byte("# My Agents")) {
		t.Fatal("expected surrounding content to be preserved")
	}
	if !bytes.Contains(data, []byte("More stuff")) {
		t.Fatal("expected trailing content to be preserved")
	}
}

func TestRunUninstall_NonInteractive(t *testing.T) {
	t.Parallel()
	targetDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(targetDir, "verda-cloud.md"), []byte("test"), 0o600)
	_ = os.WriteFile(filepath.Join(targetDir, "verda-reference.md"), []byte("test"), 0o600)
	statePath := filepath.Join(t.TempDir(), "skills.json")
	_ = SaveState(statePath, &State{
		Version:     "1.0.0",
		InstalledAt: time.Now(),
		Agents:      []string{"claude-code"},
	})

	mock := tuitest.New().AddConfirm(true)
	f := cmdutil.NewTestFactory(mock)
	var out bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &out}

	opts := &uninstallOptions{
		agents:     []string{"claude-code"},
		statePath:  statePath,
		skillNames: []string{"verda-cloud.md", "verda-reference.md"},
		agentOverrides: map[string]*Agent{
			"claude-code": {
				Name: "claude-code", DisplayName: "Claude Code",
				Scope: "global", Method: "copy", Target: targetDir,
			},
		},
	}

	if err := runUninstall(context.Background(), f, ioStreams, opts); err != nil {
		t.Fatalf("uninstall error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "verda-cloud.md")); !os.IsNotExist(err) {
		t.Fatal("expected files to be deleted")
	}
	state, _ := LoadState(statePath)
	if state.HasAgent("claude-code") {
		t.Fatal("expected claude-code removed from state")
	}
}
