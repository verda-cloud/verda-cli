package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	skillsRepo     = "verda-cloud/verda-ai-skills"
	defaultBaseURL = "https://raw.githubusercontent.com"
	branch         = "main"
	fetchTimeout   = 30 * time.Second
)

// Manifest describes the structure of the remote skill repository manifest.
type Manifest struct {
	Version string   `json:"version"`
	Skills  []string `json:"skills"`
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
