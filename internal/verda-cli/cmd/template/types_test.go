package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateSaveAndLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{
		Resource:      "vm",
		BillingType:   "on_demand",
		Contract:      "monthly",
		Kind:          "gpu",
		InstanceType:  "A100x4",
		Location:      "FIN-01",
		Image:         "ubuntu-22.04",
		OSVolumeSize:  100,
		Storage:       []StorageSpec{{Type: "ssd", Size: 500}, {Type: "hdd", Size: 1000}},
		SSHKeys:       []string{"my-key", "work-key"},
		StartupScript: "setup.sh",
	}

	if err := Save(dir, "vm", "my-template", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists at expected path.
	path := filepath.Join(dir, "vm", "my-template.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s, got error: %v", path, err)
	}

	loaded, err := Load(dir, "vm", "my-template")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify scalar fields round-trip correctly.
	assertStr(t, "Resource", loaded.Resource, tmpl.Resource)
	assertStr(t, "BillingType", loaded.BillingType, tmpl.BillingType)
	assertStr(t, "Contract", loaded.Contract, tmpl.Contract)
	assertStr(t, "Kind", loaded.Kind, tmpl.Kind)
	assertStr(t, "InstanceType", loaded.InstanceType, tmpl.InstanceType)
	assertStr(t, "Location", loaded.Location, tmpl.Location)
	assertStr(t, "Image", loaded.Image, tmpl.Image)
	assertStr(t, "StartupScript", loaded.StartupScript, tmpl.StartupScript)
	assertInt(t, "OSVolumeSize", loaded.OSVolumeSize, tmpl.OSVolumeSize)

	// Verify Storage round-trip.
	if len(loaded.Storage) != 2 {
		t.Fatalf("Storage length = %d, want 2", len(loaded.Storage))
	}
	if loaded.Storage[0].Type != "ssd" || loaded.Storage[0].Size != 500 {
		t.Errorf("Storage[0] = %+v, want {ssd 500}", loaded.Storage[0])
	}
	if loaded.Storage[1].Type != "hdd" || loaded.Storage[1].Size != 1000 {
		t.Errorf("Storage[1] = %+v, want {hdd 1000}", loaded.Storage[1])
	}

	// Verify SSHKeys round-trip.
	if len(loaded.SSHKeys) != 2 || loaded.SSHKeys[0] != "my-key" || loaded.SSHKeys[1] != "work-key" {
		t.Errorf("SSHKeys = %v, want [my-key work-key]", loaded.SSHKeys)
	}
}

func assertStr(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", field, got, want)
	}
}

func assertInt(t *testing.T, field string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", field, got, want)
	}
}

func TestLoadFromPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{
		Resource:     "vm",
		InstanceType: "H100x8",
		Location:     "US-EAST-1",
	}

	if err := Save(dir, "vm", "pathtest", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path := filepath.Join(dir, "vm", "pathtest.yaml")
	loaded, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}

	if loaded.InstanceType != "H100x8" {
		t.Errorf("InstanceType = %q, want %q", loaded.InstanceType, "H100x8")
	}
	if loaded.Location != "US-EAST-1" {
		t.Errorf("Location = %q, want %q", loaded.Location, "US-EAST-1")
	}
}

func TestLoadFromPath_NotExist(t *testing.T) {
	t.Parallel()

	_, err := LoadFromPath("/nonexistent/path/template.yaml")
	if err == nil {
		t.Fatal("LoadFromPath() expected error for nonexistent file, got nil")
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Save a few templates.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		tmpl := &Template{
			Resource:     "vm",
			InstanceType: "A100x4",
			Location:     "FIN-01",
			Image:        "ubuntu-22.04",
		}
		if err := Save(dir, "vm", name, tmpl); err != nil {
			t.Fatalf("Save(%q) error = %v", name, err)
		}
	}

	entries, err := List(dir, "vm")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("List() returned %d entries, want 3", len(entries))
	}

	// Verify entries have expected fields.
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.Resource != "vm" {
			t.Errorf("entry %q: Resource = %q, want %q", e.Name, e.Resource, "vm")
		}
		if e.Path == "" {
			t.Errorf("entry %q: Path is empty", e.Name)
		}
		if e.Description == "" {
			t.Errorf("entry %q: Description is empty", e.Name)
		}
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !names[name] {
			t.Errorf("List() missing entry %q", name)
		}
	}
}

func TestList_NonExistentDir(t *testing.T) {
	t.Parallel()

	entries, err := List("/nonexistent/base", "vm")
	if err != nil {
		t.Fatalf("List() error = %v, want nil for nonexistent dir", err)
	}
	if entries != nil {
		t.Errorf("List() = %v, want nil for nonexistent dir", entries)
	}
}

func TestListAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create templates in two resource types.
	save := func(resource, name string) {
		tmpl := &Template{Resource: resource, InstanceType: "type-" + name}
		if err := Save(dir, resource, name, tmpl); err != nil {
			t.Fatalf("Save(%q/%q) error = %v", resource, name, err)
		}
	}
	save("vm", "dev")
	save("vm", "prod")
	save("volume", "big-disk")

	entries, err := ListAll(dir)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("ListAll() returned %d entries, want 3", len(entries))
	}

	// Verify we got entries from both resource types.
	resources := map[string]int{}
	for _, e := range entries {
		resources[e.Resource]++
	}
	if resources["vm"] != 2 {
		t.Errorf("ListAll() vm count = %d, want 2", resources["vm"])
	}
	if resources["volume"] != 1 {
		t.Errorf("ListAll() volume count = %d, want 1", resources["volume"])
	}
}

func TestListAll_NonExistentDir(t *testing.T) {
	t.Parallel()

	entries, err := ListAll("/nonexistent/base")
	if err != nil {
		t.Fatalf("ListAll() error = %v, want nil for nonexistent dir", err)
	}
	if entries != nil {
		t.Errorf("ListAll() = %v, want nil for nonexistent dir", entries)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{Resource: "vm", InstanceType: "A100x4"}

	if err := Save(dir, "vm", "doomed", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify it exists.
	path := filepath.Join(dir, "vm", "doomed.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s before delete", path)
	}

	if err := Delete(dir, "vm", "doomed"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's gone.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted at %s", path)
	}
}

func TestDelete_NotExist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Delete(dir, "vm", "nonexistent")
	if err == nil {
		t.Fatal("Delete() expected error for nonexistent file, got nil")
	}
}

func TestAutoDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tmpl Template
		want string
	}{
		{
			name: "all fields",
			tmpl: Template{InstanceType: "A100x4", Image: "ubuntu-22.04", Location: "FIN-01"},
			want: "A100x4, ubuntu-22.04, FIN-01",
		},
		{
			name: "instance type only",
			tmpl: Template{InstanceType: "H100x8"},
			want: "H100x8",
		},
		{
			name: "image and location",
			tmpl: Template{Image: "debian-12", Location: "US-EAST-1"},
			want: "debian-12, US-EAST-1",
		},
		{
			name: "empty template",
			tmpl: Template{},
			want: "",
		},
		{
			name: "location only",
			tmpl: Template{Location: "EU-WEST-1"},
			want: "EU-WEST-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tmpl.AutoDescription()
			if got != tt.want {
				t.Errorf("AutoDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	t.Parallel()

	valid := []string{
		"my-template",
		"a",
		"template123",
		"123template",
		"a-b-c",
		"dev",
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		input, wantContains string
	}{
		{"", "empty"},
		{"My-Template", "lowercase"},
		{"my template", "lowercase"},
		{"my/template", "lowercase"},
		{"my_template", "lowercase"},
		{"my.template", "lowercase"},
	}
	for _, tc := range invalid {
		err := ValidateName(tc.input)
		if err == nil {
			t.Errorf("ValidateName(%q) = nil, want error containing %q", tc.input, tc.wantContains)
		} else if !strings.Contains(err.Error(), tc.wantContains) {
			t.Errorf("ValidateName(%q) = %v, want error containing %q", tc.input, err, tc.wantContains)
		}
	}
}

func TestResolveTemplatePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{Resource: "vm", InstanceType: "A100x4"}

	if err := Save(dir, "vm", "resolvable", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Resolve by name.
	path, err := Resolve(dir, "vm", "resolvable")
	if err != nil {
		t.Fatalf("Resolve() by name error = %v", err)
	}
	expected := filepath.Join(dir, "vm", "resolvable.yaml")
	if path != expected {
		t.Errorf("Resolve() by name = %q, want %q", path, expected)
	}

	// Resolve by file path (absolute).
	path, err = Resolve(dir, "vm", expected)
	if err != nil {
		t.Fatalf("Resolve() by path error = %v", err)
	}
	if path != expected {
		t.Errorf("Resolve() by path = %q, want %q", path, expected)
	}

	// Resolve with .yaml suffix (treated as file path, not a name).
	// A bare "resolvable.yaml" without a directory should error since it
	// is treated as a file path and the file doesn't exist at cwd.
	_, err = Resolve(dir, "vm", "resolvable.yaml")
	if err == nil {
		t.Fatal("Resolve() with bare .yaml suffix expected error, got nil")
	}

	// Resolve with absolute .yaml path should work.
	absYamlRef := filepath.Join(dir, "vm", "resolvable.yaml")
	path, err = Resolve(dir, "vm", absYamlRef)
	if err != nil {
		t.Fatalf("Resolve() with absolute .yaml path error = %v", err)
	}
	if path != absYamlRef {
		t.Errorf("Resolve() = %q, want %q", path, absYamlRef)
	}

	// Resolve with slash (absolute path containing /).
	slashRef := dir + "/vm/resolvable.yaml"
	path, err = Resolve(dir, "vm", slashRef)
	if err != nil {
		t.Fatalf("Resolve() with slash error = %v", err)
	}
	if path != slashRef {
		t.Errorf("Resolve() with slash = %q, want %q", path, slashRef)
	}

	// Resolve nonexistent name.
	_, err = Resolve(dir, "vm", "nonexistent")
	if err == nil {
		t.Fatal("Resolve() expected error for nonexistent name, got nil")
	}
}

func TestTemplateSaveAndLoad_AllFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{
		Resource:          "vm",
		BillingType:       "spot",
		Contract:          "PAY_AS_YOU_GO",
		Kind:              "gpu",
		InstanceType:      "H100x8",
		Location:          "FIN-03",
		Image:             "ubuntu-24.04-cuda-12.8",
		OSVolumeSize:      200,
		Storage:           []StorageSpec{{Type: "NVMe", Size: 500}, {Type: "HDD", Size: 2000}},
		StorageSkip:       true,
		SSHKeys:           []string{"key-a", "key-b", "key-c"},
		StartupScript:     "my-startup.sh",
		StartupScriptSkip: true,
		HostnamePattern:   "gpu-{random}-{location}",
	}

	if err := Save(dir, "vm", "all-fields", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(dir, "vm", "all-fields")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertStr(t, "Resource", loaded.Resource, tmpl.Resource)
	assertStr(t, "BillingType", loaded.BillingType, tmpl.BillingType)
	assertStr(t, "Contract", loaded.Contract, tmpl.Contract)
	assertStr(t, "Kind", loaded.Kind, tmpl.Kind)
	assertStr(t, "InstanceType", loaded.InstanceType, tmpl.InstanceType)
	assertStr(t, "Location", loaded.Location, tmpl.Location)
	assertStr(t, "Image", loaded.Image, tmpl.Image)
	assertInt(t, "OSVolumeSize", loaded.OSVolumeSize, tmpl.OSVolumeSize)
	assertStr(t, "StartupScript", loaded.StartupScript, tmpl.StartupScript)
	assertStr(t, "HostnamePattern", loaded.HostnamePattern, tmpl.HostnamePattern)

	if !loaded.StorageSkip {
		t.Error("StorageSkip = false, want true")
	}
	if !loaded.StartupScriptSkip {
		t.Error("StartupScriptSkip = false, want true")
	}

	if len(loaded.Storage) != 2 {
		t.Fatalf("Storage length = %d, want 2", len(loaded.Storage))
	}
	if loaded.Storage[0].Type != "NVMe" || loaded.Storage[0].Size != 500 {
		t.Errorf("Storage[0] = %+v, want {NVMe 500}", loaded.Storage[0])
	}
	if loaded.Storage[1].Type != "HDD" || loaded.Storage[1].Size != 2000 {
		t.Errorf("Storage[1] = %+v, want {HDD 2000}", loaded.Storage[1])
	}

	if len(loaded.SSHKeys) != 3 {
		t.Fatalf("SSHKeys length = %d, want 3", len(loaded.SSHKeys))
	}
	for i, want := range []string{"key-a", "key-b", "key-c"} {
		if loaded.SSHKeys[i] != want {
			t.Errorf("SSHKeys[%d] = %q, want %q", i, loaded.SSHKeys[i], want)
		}
	}
}

func TestTemplateSaveAndLoad_SkipFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{
		Resource:          "vm",
		InstanceType:      "A100x4",
		Location:          "FIN-01",
		Image:             "ubuntu-22.04",
		StorageSkip:       true,
		StartupScriptSkip: true,
	}

	if err := Save(dir, "vm", "skip-flags", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(dir, "vm", "skip-flags")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !loaded.StorageSkip {
		t.Error("StorageSkip = false, want true")
	}
	if !loaded.StartupScriptSkip {
		t.Error("StartupScriptSkip = false, want true")
	}
	// Verify other fields are still correct.
	assertStr(t, "InstanceType", loaded.InstanceType, "A100x4")
	assertStr(t, "Location", loaded.Location, "FIN-01")
}

func TestTemplateSaveAndLoad_HostnamePattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{
		Resource:        "vm",
		InstanceType:    "H100x8",
		HostnamePattern: "gpu-{random}-{location}",
	}

	if err := Save(dir, "vm", "hostname-pat", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(dir, "vm", "hostname-pat")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertStr(t, "HostnamePattern", loaded.HostnamePattern, "gpu-{random}-{location}")
}

func TestList_MultipleResourceTypes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Save templates in vm/ and cluster/ subdirs.
	save := func(resource, name string) {
		tmpl := &Template{Resource: resource, InstanceType: "type-" + name}
		if err := Save(dir, resource, name, tmpl); err != nil {
			t.Fatalf("Save(%q/%q) error = %v", resource, name, err)
		}
	}
	save("vm", "dev-vm")
	save("vm", "prod-vm")
	save("cluster", "small-cluster")
	save("cluster", "large-cluster")

	// ListAll should return entries from both resource types.
	entries, err := ListAll(dir)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("ListAll() returned %d entries, want 4", len(entries))
	}

	resources := map[string]int{}
	for _, e := range entries {
		resources[e.Resource]++
	}
	if resources["vm"] != 2 {
		t.Errorf("ListAll() vm count = %d, want 2", resources["vm"])
	}
	if resources["cluster"] != 2 {
		t.Errorf("ListAll() cluster count = %d, want 2", resources["cluster"])
	}
}

func TestValidateName_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		wantErr bool
		desc    string
	}{
		{"a", false, "single char"},
		{"123", false, "numbers only"},
		{"a-b-c-d-e-f-g-h-i-j-k-l-m-n-o-p", false, "long name with hyphens"},
		{"abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz", false, "very long name"},
		{"bad-", false, "trailing hyphen is valid per regex"}, // nameRe = ^[a-z0-9][a-z0-9-]*$
		{"-bad", true, "leading hyphen is invalid"},
		{"", true, "empty name"},
		{"UPPER", true, "uppercase letters"},
		{"has space", true, "contains space"},
		{"has.dot", true, "contains dot"},
		{"has_underscore", true, "contains underscore"},
		{"has/slash", true, "contains slash"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			err := ValidateName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateName(%q) = nil, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateName(%q) = %v, want nil", tt.input, err)
			}
		})
	}
}

func TestAutoDescription_WithSkips(t *testing.T) {
	t.Parallel()

	// A template with StorageSkip=true should still generate description
	// from InstanceType, Image, and Location.
	tmpl := Template{
		InstanceType:      "A100x4",
		Image:             "ubuntu-22.04",
		Location:          "FIN-01",
		StorageSkip:       true,
		StartupScriptSkip: true,
	}
	got := tmpl.AutoDescription()
	want := "A100x4, ubuntu-22.04, FIN-01"
	if got != want {
		t.Errorf("AutoDescription() = %q, want %q", got, want)
	}
}

func TestResolve_FilePathWithYamlSuffix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{Resource: "vm", InstanceType: "A100x4"}

	if err := Save(dir, "vm", "my-template", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// "my-template.yaml" without a directory prefix is treated as a file path
	// (because it ends with .yaml), so it should fail since the file doesn't
	// exist at that relative path.
	_, err := Resolve(dir, "vm", "my-template.yaml")
	if err == nil {
		t.Fatal("Resolve() with bare .yaml suffix expected error, got nil")
	}

	// An absolute path with .yaml suffix should succeed.
	absPath := filepath.Join(dir, "vm", "my-template.yaml")
	path, err := Resolve(dir, "vm", absPath)
	if err != nil {
		t.Fatalf("Resolve() with absolute .yaml path error = %v", err)
	}
	if path != absPath {
		t.Errorf("Resolve() = %q, want %q", path, absPath)
	}
}

func TestResolve_RelativePathWithSlash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tmpl := &Template{Resource: "vm", InstanceType: "H100x8"}

	if err := Save(dir, "vm", "test", tmpl); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// A reference containing "/" is treated as a file path.
	// Use the actual absolute path to simulate "path with slash".
	refWithSlash := dir + "/vm/test.yaml"
	path, err := Resolve(dir, "vm", refWithSlash)
	if err != nil {
		t.Fatalf("Resolve() with slash error = %v", err)
	}
	if path != refWithSlash {
		t.Errorf("Resolve() = %q, want %q", path, refWithSlash)
	}

	// A nonexistent path with slash should fail.
	_, err = Resolve(dir, "vm", "./templates/nonexistent.yaml")
	if err == nil {
		t.Fatal("Resolve() expected error for nonexistent path with slash, got nil")
	}
}

func TestExpandHostnamePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern  string
		location string
		check    func(string) bool
		desc     string
	}{
		{
			pattern: "gpu-{random}",
			check: func(s string) bool {
				return strings.HasPrefix(s, "gpu-") && len(s) > 4
			},
			desc: "prefix with random",
		},
		{
			pattern:  "train-{location}",
			location: "FIN-03",
			check:    func(s string) bool { return s == "train-fin-03" },
			desc:     "location placeholder",
		},
		{
			pattern:  "{random}-{location}",
			location: "FIN-01",
			check: func(s string) bool {
				return strings.HasSuffix(s, "-fin-01") && len(s) > 7
			},
			desc: "random + location",
		},
		{
			pattern: "static-name",
			check:   func(s string) bool { return s == "static-name" },
			desc:    "no placeholders",
		},
	}

	for _, tt := range tests {
		result := ExpandHostnamePattern(tt.pattern, tt.location)
		if !tt.check(result) {
			t.Errorf("%s: ExpandHostnamePattern(%q, %q) = %q, unexpected",
				tt.desc, tt.pattern, tt.location, result)
		}
	}
}
