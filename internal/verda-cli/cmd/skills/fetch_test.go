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
	"testing"
)

func TestLoadManifest(t *testing.T) {
	t.Parallel()
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Version == "" {
		t.Fatal("expected non-empty version")
	}
	if len(m.Skills) == 0 {
		t.Fatal("expected at least one skill")
	}
	if len(m.Agents) == 0 {
		t.Fatal("expected at least one agent")
	}
	cc, ok := m.Agents["claude-code"]
	if !ok {
		t.Fatal("expected claude-code agent")
	}
	if cc.Name != "claude-code" {
		t.Fatalf("expected agent Name 'claude-code', got %q", cc.Name)
	}
}

func TestLoadSkillFiles(t *testing.T) {
	t.Parallel()
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	files, err := LoadSkillFiles(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != len(m.Skills) {
		t.Fatalf("expected %d files, got %d", len(m.Skills), len(files))
	}
	for name, content := range files {
		if content == "" {
			t.Fatalf("expected non-empty content for skill %q", name)
		}
	}
}

func TestMergeUserAgents(t *testing.T) {
	t.Parallel()
	// mergeUserAgents should be a no-op when ~/.verda/agents.json doesn't exist.
	// Verify the manifest still has its built-in agents after the call.
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if _, ok := m.Agents["claude-code"]; !ok {
		t.Fatal("expected claude-code agent to survive mergeUserAgents")
	}
	if len(m.Agents) == 0 {
		t.Fatal("expected agents to survive mergeUserAgents")
	}
}

func TestManifestAgentNames(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Agents: map[string]*Agent{
			"cursor":      {Name: "cursor"},
			"claude-code": {Name: "claude-code"},
			"codex":       {Name: "codex"},
		},
	}
	names := m.AgentNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "claude-code" {
		t.Fatalf("expected claude-code first, got %q", names[0])
	}
}

func TestAgentTargetDir_Copy(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "cursor", Target: ".cursor/rules/", Method: "copy"}
	if dir := a.TargetDir(); dir != ".cursor/rules/" {
		t.Fatalf("expected '.cursor/rules/', got %q", dir)
	}
}

func TestAgentTargetDir_Append(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "codex", Target: "AGENTS.md", Method: methodAppend}
	if dir := a.TargetDir(); dir != "." {
		t.Fatalf("expected '.', got %q", dir)
	}
	if f := a.TargetFile(); f != "AGENTS.md" {
		t.Fatalf("expected 'AGENTS.md', got %q", f)
	}
}

func TestAgentDisplayLabel(t *testing.T) {
	t.Parallel()
	a := &Agent{DisplayName: "Claude Code", Target: "~/.claude/skills/"}
	label := a.DisplayLabel()
	if label != "Claude Code (~/.claude/skills/)" {
		t.Fatalf("unexpected label: %q", label)
	}
}

func TestAgentDestName(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "codex", FileMap: map[string]string{"verda-cloud.md": "SKILL.md"}}
	if got := a.DestName("verda-cloud.md"); got != "SKILL.md" {
		t.Fatalf("expected SKILL.md, got %q", got)
	}
	if got := a.DestName("verda-reference.md"); got != "verda-reference.md" {
		t.Fatalf("expected verda-reference.md (no mapping), got %q", got)
	}
	// No file_map
	b := &Agent{Name: "cursor"}
	if got := b.DestName("verda-cloud.md"); got != "verda-cloud.md" {
		t.Fatalf("expected verda-cloud.md (nil map), got %q", got)
	}
}

func TestExpandHome(t *testing.T) {
	t.Parallel()
	if p := expandHome(".cursor/rules/"); p != ".cursor/rules/" {
		t.Fatalf("expected unchanged path, got %q", p)
	}
	expanded := expandHome("~/.claude/skills/")
	if expanded == "~/.claude/skills/" {
		t.Fatal("expected ~ to be expanded")
	}
}
