package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "v1.6.1 < v1.6.2", a: "v1.6.1", b: "v1.6.2", want: -1},
		{name: "equal versions", a: "v1.6.2", b: "v1.6.2", want: 0},
		{name: "v1.7.0 > v1.6.2", a: "v1.7.0", b: "v1.6.2", want: 1},
		{name: "v2.0.0 > v1.99.99", a: "v2.0.0", b: "v1.99.99", want: 1},
		{name: "without v prefix", a: "1.6.1", b: "1.6.2", want: -1},
		{name: "pre-release suffix stripped", a: "v1.0.0-dev", b: "v1.0.0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestVersionCache_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "version-check.json")

	now := time.Now().Truncate(time.Second) // JSON loses sub-second precision
	original := &VersionCache{
		LatestVersion: "v1.6.2",
		CheckedAt:     now,
	}

	if err := SaveVersionCache(path, original); err != nil {
		t.Fatalf("SaveVersionCache: %v", err)
	}

	loaded, err := LoadVersionCache(path)
	if err != nil {
		t.Fatalf("LoadVersionCache: %v", err)
	}

	if loaded.LatestVersion != original.LatestVersion {
		t.Fatalf("LatestVersion = %q, want %q", loaded.LatestVersion, original.LatestVersion)
	}
	// Compare with second precision since JSON round-trips may lose nanos.
	if loaded.CheckedAt.Unix() != original.CheckedAt.Unix() {
		t.Fatalf("CheckedAt = %v, want %v", loaded.CheckedAt, original.CheckedAt)
	}
}

func TestVersionCache_MissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nonexistent", "version-check.json")

	cache, err := LoadVersionCache(path)
	if err != nil {
		t.Fatalf("LoadVersionCache should not error on missing file, got: %v", err)
	}
	if cache.LatestVersion != "" {
		t.Fatalf("LatestVersion should be empty, got %q", cache.LatestVersion)
	}
}

func TestVersionCache_Stale(t *testing.T) {
	t.Parallel()

	ttl := 24 * time.Hour

	t.Run("25h old is stale", func(t *testing.T) {
		c := &VersionCache{
			LatestVersion: "v1.0.0",
			CheckedAt:     time.Now().Add(-25 * time.Hour),
		}
		if !c.IsStale(ttl) {
			t.Fatal("expected cache to be stale")
		}
	})

	t.Run("1h old is fresh", func(t *testing.T) {
		c := &VersionCache{
			LatestVersion: "v1.0.0",
			CheckedAt:     time.Now().Add(-1 * time.Hour),
		}
		if c.IsStale(ttl) {
			t.Fatal("expected cache to be fresh")
		}
	})

	t.Run("empty cache is stale", func(t *testing.T) {
		c := &VersionCache{}
		if !c.IsStale(ttl) {
			t.Fatal("expected empty cache to be stale")
		}
	})
}

func TestSaveVersionCache_CreatesParentDirs(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "version-check.json")

	c := &VersionCache{LatestVersion: "v1.0.0", CheckedAt: time.Now()}
	if err := SaveVersionCache(path, c); err != nil {
		t.Fatalf("SaveVersionCache should create parent dirs, got: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}
}
