package options

import (
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// SaveSetting writes a single key-value pair to ~/.verda/config.yaml.
// The key uses dot notation (e.g. "settings.theme").
func SaveSetting(key string, value any) error {
	dir, err := EnsureVerdaDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.yaml")

	cfg := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	setNested(cfg, key, value)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return WriteSecureFile(path, data)
}

// GetSetting reads a single value from ~/.verda/config.yaml.
func GetSetting(key string) (any, bool) {
	dir, err := VerdaDir()
	if err != nil {
		return nil, false
	}
	path := filepath.Join(dir, "config.yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	cfg := map[string]any{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, false
	}

	return getNested(cfg, key)
}

// setNested sets a value in a nested map using dot notation.
func setNested(m map[string]any, key string, value any) {
	parts := splitKey(key)
	for i, part := range parts {
		if i == len(parts)-1 {
			m[part] = value
			return
		}
		child, ok := m[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			m[part] = child
		}
		m = child
	}
}

// getNested gets a value from a nested map using dot notation.
func getNested(m map[string]any, key string) (any, bool) {
	parts := splitKey(key)
	for i, part := range parts {
		if i == len(parts)-1 {
			v, ok := m[part]
			return v, ok
		}
		child, ok := m[part].(map[string]any)
		if !ok {
			return nil, false
		}
		m = child
	}
	return nil, false
}

func splitKey(key string) []string {
	var parts []string
	start := 0
	for i, c := range key {
		if c == '.' {
			if i > start {
				parts = append(parts, key[start:i])
			}
			start = i + 1
		}
	}
	if start < len(key) {
		parts = append(parts, key[start:])
	}
	return parts
}
