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

package inference

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	stateVersion = 1

	fileKindOwned   = "owned"
	fileKindPatched = "patched"
)

// agentsState is ~/.verda/inference-agents.yaml. Agent config files are
// machine-global, so an agent belongs to at most one profile at a time.
type agentsState struct {
	Version  int                      `yaml:"version"`
	RCFile   string                   `yaml:"rc_file,omitempty"`
	Profiles map[string]*profileState `yaml:"profiles,omitempty"`
}

type profileState struct {
	Agents map[string]*agentRecord `yaml:"agents,omitempty"`
}

type agentRecord struct {
	KeyID     string       `yaml:"key_id"`
	KeyLabel  string       `yaml:"key_label"`
	Models    []string     `yaml:"models"`
	BaseURL   string       `yaml:"base_url"`
	Files     []fileRecord `yaml:"files"`
	UpdatedAt time.Time    `yaml:"updated_at"`
}

type fileRecord struct {
	Path string `yaml:"path"`
	Kind string `yaml:"kind"`
	// Backup holds the pre-verda content of a file that existed before setup.
	Backup string `yaml:"backup,omitempty"`
	// Created: setup created this file, so remove may delete it once empty.
	Created bool `yaml:"created,omitempty"`
}

func loadState(path string) (*agentsState, error) {
	data, exists, err := readFileIfExists(path)
	if err != nil {
		return nil, err
	}
	s := &agentsState{Version: stateVersion}
	if exists {
		if err := yaml.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", path, err)
		}
	}
	if s.Profiles == nil {
		s.Profiles = map[string]*profileState{}
	}
	return s, nil
}

func saveState(path string, s *agentsState) error {
	for name, p := range s.Profiles {
		if p == nil || len(p.Agents) == 0 {
			delete(s.Profiles, name)
		}
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600, true)
}

func (s *agentsState) agent(profile, agent string) *agentRecord {
	if p := s.Profiles[profile]; p != nil {
		return p.Agents[agent]
	}
	return nil
}

func (s *agentsState) setAgent(profile, agent string, rec *agentRecord) {
	p := s.Profiles[profile]
	if p == nil {
		p = &profileState{}
		s.Profiles[profile] = p
	}
	if p.Agents == nil {
		p.Agents = map[string]*agentRecord{}
	}
	p.Agents[agent] = rec
}

func (s *agentsState) removeAgent(profile, agent string) {
	if p := s.Profiles[profile]; p != nil {
		delete(p.Agents, agent)
	}
}

// owner returns the profile that set up agent, "" if none.
func (s *agentsState) owner(agent string) string {
	for _, name := range slices.Sorted(maps.Keys(s.Profiles)) {
		if p := s.Profiles[name]; p != nil && p.Agents[agent] != nil {
			return name
		}
	}
	return ""
}

func (s *agentsState) agentNames(profile string) []string {
	p := s.Profiles[profile]
	if p == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(p.Agents))
}

// allAgents lists every set-up agent across profiles.
func (s *agentsState) allAgents() []string {
	var out []string
	for _, p := range s.Profiles {
		if p == nil {
			continue
		}
		for name := range p.Agents {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func (r *agentRecord) file(path string) *fileRecord {
	if r == nil {
		return nil
	}
	for i := range r.Files {
		if r.Files[i].Path == path {
			return &r.Files[i]
		}
	}
	return nil
}

// allAgentsWith is every agent in state plus extra (the ones being set up).
func (s *agentsState) allAgentsWith(extra []string) []string {
	out := s.allAgents()
	for _, n := range extra {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
