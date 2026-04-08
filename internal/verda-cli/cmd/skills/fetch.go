package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	skillsRepo       = "verda-cloud/verda-ai-skills"
	defaultBaseURL   = "https://raw.githubusercontent.com"
	branch           = "main"
	fetchTimeout     = 30 * time.Second
	defaultAgentName = "claude-code"
)

// Manifest describes the structure of the remote skill repository manifest.
type Manifest struct {
	Version string            `json:"version"`
	Skills  []string          `json:"skills"`
	Agents  map[string]*Agent `json:"agents"`
}

// Agent describes an AI coding agent target for skill installation.
// Agent definitions are fetched from the manifest, not hardcoded.
type Agent struct {
	Name        string `json:"-"` // set from the map key
	DisplayName string `json:"display_name"`
	Scope       string `json:"scope"`  // "global" or "project"
	Target      string `json:"target"` // path with ~ expansion, or filename for append
	Method      string `json:"method"` // "copy" or "append"
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

type fetcher struct {
	baseURL string
	client  *http.Client
}

// NewFetcher creates a fetcher configured for the production skills repository.
func NewFetcher() *fetcher {
	return &fetcher{
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: fetchTimeout},
	}
}

// FetchManifest downloads and parses manifest.json from the skills repository.
func (f *fetcher) FetchManifest(ctx context.Context) (*Manifest, error) {
	url := fmt.Sprintf("%s/%s/%s/manifest.json", f.baseURL, skillsRepo, branch)
	data, err := f.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	// Populate agent Name field from map keys.
	for name, agent := range m.Agents {
		agent.Name = name
	}
	return &m, nil
}

// FetchSkillFile downloads a single skill file by name from the skills repository.
func (f *fetcher) FetchSkillFile(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/skills/%s", f.baseURL, skillsRepo, branch, name)
	data, err := f.get(ctx, url)
	if err != nil {
		return "", fmt.Errorf("fetching skill %s: %w", name, err)
	}
	return string(data), nil
}

func (f *fetcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
