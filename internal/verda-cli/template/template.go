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
	Description       string        `yaml:"description,omitempty"`

	// Container carries serverless-container settings; nil for other resources.
	Container *ContainerSpec `yaml:"container,omitempty"`
}

// ContainerSpec mirrors the container create parameters. Field names match
// the CLI flags so a hand-edited template is guessable.
type ContainerSpec struct {
	Spot            bool              `yaml:"spot,omitempty"`
	Compute         string            `yaml:"compute,omitempty"`
	ComputeSize     int               `yaml:"compute_size,omitempty"`
	Image           string            `yaml:"image,omitempty"`
	RegistryCreds   string            `yaml:"registry_creds,omitempty"`
	Port            int               `yaml:"port,omitempty"`
	HealthcheckOff  bool              `yaml:"healthcheck_off,omitempty"`
	HealthcheckPort int               `yaml:"healthcheck_port,omitempty"`
	HealthcheckPath string            `yaml:"healthcheck_path,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	EnvSecret       map[string]string `yaml:"env_secret,omitempty"`
	Entrypoint      []string          `yaml:"entrypoint,omitempty"`
	Cmd             []string          `yaml:"cmd,omitempty"`
	// MinReplicas is a pointer because 0 (scale-to-zero) is meaningful:
	// omitempty on a plain int would silently drop it.
	MinReplicas    *int     `yaml:"min_replicas,omitempty"`
	MaxReplicas    int      `yaml:"max_replicas,omitempty"`
	Concurrency    int      `yaml:"concurrency,omitempty"`
	QueuePreset    string   `yaml:"queue_preset,omitempty"`
	QueueLoad      int      `yaml:"queue_load,omitempty"`
	CPUUtil        int      `yaml:"cpu_util,omitempty"`
	GPUUtil        int      `yaml:"gpu_util,omitempty"`
	ScaleUpDelay   string   `yaml:"scale_up_delay,omitempty"` // Go duration string
	ScaleDownDelay string   `yaml:"scale_down_delay,omitempty"`
	RequestTTL     string   `yaml:"request_ttl,omitempty"`
	SecretMounts   []string `yaml:"secret_mounts,omitempty"` // "SECRET:/path"
	// A `model:` / `variants:` block lands here when the model-runtime design
	// ships — deliberately absent until then.
}

// vmOnlyFields are Template fields that must stay empty on non-VM templates.
// Checked by Validate; names match the YAML keys for error messages.
func (t *Template) vmOnlyFieldsSet() []string {
	var bad []string
	if t.BillingType != "" {
		bad = append(bad, "billing_type")
	}
	if t.Contract != "" {
		bad = append(bad, "contract")
	}
	if t.Kind != "" {
		bad = append(bad, "kind")
	}
	if t.InstanceType != "" {
		bad = append(bad, "instance_type")
	}
	if t.Location != "" {
		bad = append(bad, "location")
	}
	if t.Image != "" {
		bad = append(bad, "image")
	}
	if t.OSVolumeSize != 0 {
		bad = append(bad, "os_volume_size")
	}
	if len(t.Storage) > 0 {
		bad = append(bad, "storage")
	}
	if t.StorageSkip {
		bad = append(bad, "storage_skip")
	}
	if len(t.SSHKeys) > 0 {
		bad = append(bad, "ssh_keys")
	}
	if t.StartupScript != "" {
		bad = append(bad, "startup_script")
	}
	if t.StartupScriptSkip {
		bad = append(bad, "startup_script_skip")
	}
	if t.HostnamePattern != "" {
		bad = append(bad, "hostname_pattern")
	}
	return bad
}

// Validate enforces the per-resource shape. resource "" is read as "vm":
// every template written before the container split lives under vm/ anyway.
// Anything else unknown is rejected, and a mismatched container block/VM
// fields are named in the error.
func (t *Template) Validate() error {
	resource := t.Resource
	if resource == "" {
		resource = "vm"
	}
	switch resource {
	case "vm":
		if t.Container != nil {
			return errors.New(`resource "vm" must not set the "container" block`)
		}
	case "container":
		if t.Container == nil {
			return errors.New(`resource "container" requires the "container" block`)
		}
		if bad := t.vmOnlyFieldsSet(); len(bad) > 0 {
			return fmt.Errorf("container template must not set VM fields: %s", strings.Join(bad, ", "))
		}
	default:
		return fmt.Errorf("unknown resource %q (supported: vm, container)", t.Resource)
	}
	return nil
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // templates are not secrets
		return fmt.Errorf("writing template file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("saving template file: %w", err)
	}
	return nil
}

// Load reads a template from baseDir/<resource>/<name>.yaml.
func Load(baseDir, resource, name string) (*Template, error) {
	path := filepath.Join(baseDir, resource, name+".yaml")
	return LoadFromPath(path)
}

// LoadFromPath reads a template from an absolute file path and validates it.
// Validation errors name the file so a bad template fails loudly before deploy.
func LoadFromPath(path string) (*Template, error) {
	return loadTemplate(path, true)
}

// List/ListAll use loadTemplate(..., false): the listing stays
// forward-compatible with resource kinds added after this CLI version.
func loadTemplate(path string, validate bool) (*Template, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-provided template path
	if err != nil {
		return nil, fmt.Errorf("reading template file: %w", err)
	}

	var tmpl Template
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parsing template file %s: %w", path, err)
	}
	if validate {
		if err := tmpl.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
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
		return "", fmt.Errorf("template name is required — template %q not found. Run \"verda template list\" to see available templates, or use \"--from\" to pick interactively", ref)
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

		tmpl, err := loadTemplate(path, false)
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

// AutoDescription returns a human-readable summary. If the user provided a
// custom Description it is returned as-is; otherwise the method falls back to
// joining non-empty InstanceType, Image, and Location with ", ".
func (t *Template) AutoDescription() string {
	if t.Description != "" {
		return t.Description
	}
	var parts []string
	for _, s := range []string{t.InstanceType, t.Image, t.Location} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}
