package skills

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// State tracks which skills version is installed and for which agents.
type State struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
	Agents      []string  `json:"agents"`
}

// HasAgent reports whether the given agent name is in the installed list.
func (s *State) HasAgent(name string) bool {
	return slices.Contains(s.Agents, name)
}

// RemoveAgent removes the named agent from the installed list.
func (s *State) RemoveAgent(name string) {
	filtered := s.Agents[:0]
	for _, a := range s.Agents {
		if a != name {
			filtered = append(filtered, a)
		}
	}
	s.Agents = filtered
}

// StatePath returns the default path for the skills state file (~/.verda/skills.json).
func StatePath() (string, error) {
	dir, err := clioptions.VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills.json"), nil
}

// LoadState reads the skills state from the given path.
// If the file does not exist, it returns an empty State (not an error).
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveState writes the skills state to the given path, creating parent
// directories as needed.
func SaveState(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
