package template

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
	"go.yaml.in/yaml/v3"
)

func generateRandomWords() string {
	return petname.Generate(3, "-")
}

// Template represents a saved configuration template for creating resources.
type Template struct {
	Resource          string        `yaml:"resource"`
	BillingType       string        `yaml:"billing_type,omitempty"`
	Contract          string        `yaml:"contract,omitempty"`
	Kind              string        `yaml:"kind,omitempty"`
	InstanceType      string        `yaml:"instance_type,omitempty"`
	Location          string        `yaml:"location,omitempty"`
	Image             string        `yaml:"image,omitempty"`
	OSVolumeSize      int           `yaml:"os_volume_size,omitempty"`
	Storage           []StorageSpec `yaml:"storage,omitempty"`
	StorageSkip       bool          `yaml:"storage_skip,omitempty"`
	SSHKeys           []string      `yaml:"ssh_keys,omitempty"`
	StartupScript     string        `yaml:"startup_script,omitempty"`
	StartupScriptSkip bool          `yaml:"startup_script_skip,omitempty"`
	HostnamePattern   string        `yaml:"hostname_pattern,omitempty"`
}

// StorageSpec describes an additional storage volume attached to a template.
type StorageSpec struct {
	Type string `yaml:"type"`
	Size int    `yaml:"size"`
}

// Entry represents a template listing entry with metadata.
type Entry struct {
	Resource    string `json:"resource" yaml:"resource"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Path        string `json:"path" yaml:"path"`
}

// ExpandHostnamePattern expands placeholders in a hostname pattern:
//   - {random} → 3 random words joined by hyphens (e.g. "cold-cable-smiles")
//   - {location} → lowercased location code (e.g. "fin-03")
//
// Example: "gpu-{random}-{location}" → "gpu-cold-cable-smiles-fin-03".
func ExpandHostnamePattern(pattern, locationCode string) string {
	s := pattern
	for strings.Contains(s, "{random}") {
		words := generateRandomWords()
		s = strings.Replace(s, "{random}", words, 1)
	}
	s = strings.ReplaceAll(s, "{location}", strings.ToLower(locationCode))
	return s
}

// nameRe matches valid template names: lowercase alphanumeric and hyphens.
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidateName checks that name is non-empty and contains only lowercase
// alphanumeric characters and hyphens.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("template name must not be empty")
	}
	if !nameRe.MatchString(name) {
		return errors.New("template name must contain only lowercase letters, digits, and hyphens")
	}
	return nil
}

// Save writes a template to baseDir/<resource>/<name>.yaml.
// Directories are created with 0700 permissions; files with 0644.
func Save(baseDir, resource, name string, tmpl *Template) error {
	dir := filepath.Join(baseDir, resource)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating template directory: %w", err)
	}

	data, err := yaml.Marshal(tmpl)
	if err != nil {
		return fmt.Errorf("marshaling template: %w", err)
	}

	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // templates are not secrets
		return fmt.Errorf("writing template file: %w", err)
	}
	return nil
}

// Load reads a template from baseDir/<resource>/<name>.yaml.
func Load(baseDir, resource, name string) (*Template, error) {
	path := filepath.Join(baseDir, resource, name+".yaml")
	return LoadFromPath(path)
}

// LoadFromPath reads a template from an absolute file path.
func LoadFromPath(path string) (*Template, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-provided template path
	if err != nil {
		return nil, fmt.Errorf("reading template file: %w", err)
	}

	var tmpl Template
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parsing template file: %w", err)
	}
	return &tmpl, nil
}

// Resolve converts a template reference to an absolute file path.
// If ref contains "/" or ends with ".yaml", it is treated as a file path;
// otherwise it is resolved as baseDir/<resource>/<ref>.yaml.
// The resolved path must exist.
func Resolve(baseDir, resource, ref string) (string, error) {
	var path string
	if strings.Contains(ref, "/") || strings.HasSuffix(ref, ".yaml") {
		path = ref
	} else {
		path = filepath.Join(baseDir, resource, ref+".yaml")
	}

	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("template not found: %w", err)
	}
	return path, nil
}

// List returns all template entries in baseDir/<resource>/.
// Returns nil (not an error) if the directory does not exist.
func List(baseDir, resource string) ([]Entry, error) {
	dir := filepath.Join(baseDir, resource)
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading template directory: %w", err)
	}

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(de.Name(), ".yaml")
		path := filepath.Join(dir, de.Name())

		tmpl, err := LoadFromPath(path)
		if err != nil {
			continue // skip unparseable files
		}

		entries = append(entries, Entry{
			Resource:    resource,
			Name:        name,
			Description: tmpl.AutoDescription(),
			Path:        path,
		})
	}
	return entries, nil
}

// ListAll returns template entries across all resource subdirectories.
// Returns nil (not an error) if baseDir does not exist.
func ListAll(baseDir string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading templates base directory: %w", err)
	}

	var entries []Entry
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		sub, err := List(baseDir, de.Name())
		if err != nil {
			return nil, err
		}
		entries = append(entries, sub...)
	}
	return entries, nil
}

// Delete removes a template file at baseDir/<resource>/<name>.yaml.
func Delete(baseDir, resource, name string) error {
	path := filepath.Join(baseDir, resource, name+".yaml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting template: %w", err)
	}
	return nil
}

// AutoDescription returns a human-readable summary by joining non-empty
// InstanceType, Image, and Location fields with ", ".
func (t *Template) AutoDescription() string {
	var parts []string
	for _, s := range []string{t.InstanceType, t.Image, t.Location} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
