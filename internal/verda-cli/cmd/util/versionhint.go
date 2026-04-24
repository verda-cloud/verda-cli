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

package util

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
	"github.com/verda-cloud/verdagostack/pkg/version"
)

// VersionCache holds the result of the last version check so we can avoid
// hitting the GitHub API on every CLI invocation.
type VersionCache struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

// IsStale reports whether the cache is older than ttl.
func (c *VersionCache) IsStale(ttl time.Duration) bool {
	if c.LatestVersion == "" {
		return true
	}
	return time.Since(c.CheckedAt) > ttl
}

// VersionCachePath returns the path to ~/.verda/version-check.json.
func VersionCachePath() (string, error) {
	dir, err := clioptions.VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "version-check.json"), nil
}

// LoadVersionCache reads the version cache from disk.
// Returns an empty cache (no error) if the file is missing or corrupt.
func LoadVersionCache(path string) (*VersionCache, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		// Missing file is not an error — just return empty cache.
		return &VersionCache{}, nil //nolint:nilerr // intentional: missing file → empty cache
	}
	var c VersionCache
	if err := json.Unmarshal(data, &c); err != nil {
		// Corrupt file — return empty cache.
		return &VersionCache{}, nil //nolint:nilerr // intentional: corrupt file → empty cache
	}
	return &c, nil
}

// SaveVersionCache writes the version cache to disk, creating parent
// directories with 0700 permissions if needed.
func SaveVersionCache(path string, c *VersionCache) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling version cache: %w", err)
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // version cache is not sensitive
}

// FetchLatestVersion queries the GitHub releases API for the latest release
// tag of verda-cli. The per-request timeout is intentionally tight (2s): the
// only callers are `doctor`, `update`, and help/root — if GitHub is slow or
// unreachable we'd rather skip the hint than make the user wait, and a live
// CLI on a reachable network comfortably returns in well under 2s.
func FetchLatestVersion(ctx context.Context) (string, error) {
	const url = "https://api.github.com/repos/verda-cloud/verda-cli/releases/latest"

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "verda-cli/"+version.Get().GitVersion)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decoding release response: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("no tag_name in release response")
	}
	return release.TagName, nil
}

// CheckVersion loads the version cache, fetches if stale (24h TTL), saves the
// cache, and returns the latest and current versions. On fetch error it falls
// back to the cached value.
func CheckVersion(ctx context.Context) (latest, current string, err error) {
	const ttl = 24 * time.Hour

	cachePath, err := VersionCachePath()
	if err != nil {
		return "", "", err
	}

	cache, err := LoadVersionCache(cachePath)
	if err != nil {
		return "", "", err
	}

	latest = cache.LatestVersion

	if cache.IsStale(ttl) {
		fetched, fetchErr := FetchLatestVersion(ctx)
		switch {
		case fetchErr != nil && cache.LatestVersion == "":
			return "", "", fetchErr
		case fetchErr == nil:
			latest = fetched
			cache.LatestVersion = fetched
			cache.CheckedAt = time.Now()
			_ = SaveVersionCache(cachePath, cache) // best-effort
		}
		// fetchErr != nil && cache.LatestVersion != "": fall back to cached value
	}

	current = version.Get().GitVersion
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}

	return latest, current, nil
}

// PrintVersionHint prints an update hint to w if latest > current.
func PrintVersionHint(w io.Writer, latest, current string) {
	if CompareVersions(latest, current) > 0 {
		_, _ = fmt.Fprintf(w, "\nUpdate available: %s → %s — run 'verda update'\n", current, latest)
	}
}

// CompareVersions performs a simple semver comparison, returning -1, 0, or 1.
// It strips "v" prefixes and pre-release suffixes (e.g. "-dev").
func CompareVersions(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)

	for i := 0; i < 3; i++ {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseSemver strips "v" prefix and pre-release suffixes, then splits into
// three integer components [major, minor, patch].
func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)

	var result [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		// Strip pre-release suffix (e.g. "0-dev" -> "0").
		clean := parts[i]
		if idx := strings.IndexByte(clean, '-'); idx >= 0 {
			clean = clean[:idx]
		}
		n, _ := strconv.Atoi(clean)
		result[i] = n
	}
	return result
}
