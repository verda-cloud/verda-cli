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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	embeddedskills "github.com/verda-cloud/verda-cli/internal/skills"
)

const defaultAgentName = "claude-code"

// Manifest describes the structure of the skill repository manifest.
type Manifest struct {
	Version string            `json:"version"`
	Skills  []string          `json:"skills"`
	Agents  map[string]*Agent `json:"agents"`
}

// Agent describes an AI coding agent target for skill installation.
// Agent definitions come from the embedded manifest and optional user overrides.
type Agent struct {
	Name        string            `json:"-"` // set from the map key
	DisplayName string            `json:"display_name"`
	Scope       string            `json:"scope"`              // "global" or "project"
	Target      string            `json:"target"`             // path with ~ expansion, or filename for append
	Method      string            `json:"method"`             // "copy" or "append"
	FileMap     map[string]string `json:"file_map,omitempty"` // optional: rename files during install (src -> dst)
}

// TargetDir returns the resolved directory path for this agent.
// For "copy" agents, it's the directory to copy files into.
// For "append" agents, it's the directory containing the target file.
func (a *Agent) TargetDir() string {
	target := expandHome(a.Target)
	if a.Method == methodAppend {
		return filepath.Dir(target)
	}
	return target
}

// TargetFile returns the filename for append-method agents.
func (a *Agent) TargetFile() string {
	return filepath.Base(a.Target)
}

// DisplayLabel returns a human-readable label for prompts.
func (a *Agent) DisplayLabel() string {
	return a.DisplayName + " (" + a.Target + ")"
}

// DestName returns the destination filename for a skill file, applying
// the agent's FileMap rename if one exists.
func (a *Agent) DestName(src string) string {
	if a.FileMap != nil {
		if dst, ok := a.FileMap[src]; ok {
			return dst
		}
	}
	return src
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// AgentNames returns sorted agent names from the manifest.
func (m *Manifest) AgentNames() []string {
	// Return in a stable order: claude-code first, then alphabetical.
	names := make([]string, 0, len(m.Agents))
	if _, ok := m.Agents[defaultAgentName]; ok {
		names = append(names, defaultAgentName)
	}
	sorted := make([]string, 0, len(m.Agents))
	for name := range m.Agents {
		if name != defaultAgentName {
			sorted = append(sorted, name)
		}
	}
	// Simple insertion sort for small list.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	names = append(names, sorted...)
	return names
}

// AgentDisplayLabels returns human-readable labels in the same order as AgentNames.
func (m *Manifest) AgentDisplayLabels() []string {
	names := m.AgentNames()
	labels := make([]string, len(names))
	for i, name := range names {
		labels[i] = m.Agents[name].DisplayLabel()
	}
	return labels
}

// LoadManifest parses the embedded manifest and merges user-defined agents.
func LoadManifest() (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(embeddedskills.ManifestData(), &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	// Populate agent Name field from map keys.
	for name, agent := range m.Agents {
		agent.Name = name
	}
	mergeUserAgents(&m)
	return &m, nil
}

// LoadSkillFiles reads all skill files listed in the manifest from the embedded filesystem.
func LoadSkillFiles(m *Manifest) (map[string]string, error) {
	files := make(map[string]string, len(m.Skills))
	for _, name := range m.Skills {
		content, err := embeddedskills.ReadSkillFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading skill %s: %w", name, err)
		}
		files[name] = content
	}
	return files, nil
}

// userAgentsFile is the structure of ~/.verda/agents.json.
type userAgentsFile struct {
	Agents map[string]*Agent `json:"agents"`
}

// mergeUserAgents reads ~/.verda/agents.json and merges user-defined agents into
// the manifest. User entries override built-in agents with the same key.
// Silent no-op if the file doesn't exist or is malformed.
func mergeUserAgents(m *Manifest) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(home, ".verda", "agents.json")))
	if err != nil {
		return
	}
	var uf userAgentsFile
	if err := json.Unmarshal(data, &uf); err != nil {
		return
	}
	for name, agent := range uf.Agents {
		agent.Name = name
		m.Agents[name] = agent
	}
}
