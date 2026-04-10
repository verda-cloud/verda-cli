package template

import (
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
)

func TestNormalizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"My Template", "my-template"},
		{"--bad--name--", "bad-name"},
		{"UPPER_CASE", "upper-case"},
		{"already-good", "already-good"},
		{"multiple   spaces", "multiple-spaces"},
		{"  leading-trailing  ", "leading-trailing"},
		{"dots.and.dots", "dotsanddots"},
		{"MiXeD CaSe_UnDeR", "mixed-case-under"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input        string
		wantResource string
		wantName     string
		wantErr      bool
	}{
		{"vm/gpu-training", "vm", "gpu-training", false},
		{"cluster/big-job", "cluster", "big-job", false},
		{"vm/a-b-c", "vm", "a-b-c", false},
		// Invalid refs
		{"gpu-training", "", "", true},  // no slash
		{"/gpu-training", "", "", true}, // empty resource
		{"vm/", "", "", true},           // empty name
		{"", "", "", true},              // empty string
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			resource, name, err := parseRef(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRef(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if resource != tt.wantResource || name != tt.wantName {
				t.Errorf("parseRef(%q) = (%q, %q), want (%q, %q)",
					tt.input, resource, name, tt.wantResource, tt.wantName)
			}
		})
	}
}

func TestVmResultToTemplate(t *testing.T) {
	t.Parallel()
	result := &vm.TemplateResult{
		BillingType:       "on-demand",
		Contract:          "PAY_AS_YOU_GO",
		Kind:              "GPU",
		InstanceType:      "1V100.6V",
		Location:          "FIN-01",
		Image:             "ubuntu-24.04",
		OSVolumeSize:      100,
		SSHKeyNames:       []string{"my-key"},
		StartupScriptName: "init-script",
		StorageSize:       500,
		StorageType:       "NVMe",
		StorageSkip:       false,
		StartupScriptSkip: false,
	}

	tmpl := vmResultToTemplate(result)

	assertStr := func(field, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}

	assertStr("Resource", tmpl.Resource, "vm")
	assertStr("BillingType", tmpl.BillingType, "on-demand")
	assertStr("Contract", tmpl.Contract, "PAY_AS_YOU_GO")
	assertStr("Kind", tmpl.Kind, "GPU")
	assertStr("InstanceType", tmpl.InstanceType, "1V100.6V")
	assertStr("Location", tmpl.Location, "FIN-01")
	assertStr("Image", tmpl.Image, "ubuntu-24.04")

	if tmpl.OSVolumeSize != 100 {
		t.Errorf("OSVolumeSize = %d, want 100", tmpl.OSVolumeSize)
	}
	if len(tmpl.SSHKeys) != 1 || tmpl.SSHKeys[0] != "my-key" {
		t.Errorf("SSHKeys = %v, want [my-key]", tmpl.SSHKeys)
	}
	assertStr("StartupScript", tmpl.StartupScript, "init-script")

	if len(tmpl.Storage) != 1 {
		t.Fatalf("Storage len = %d, want 1", len(tmpl.Storage))
	}
	assertStr("Storage[0].Type", tmpl.Storage[0].Type, "NVMe")
	if tmpl.Storage[0].Size != 500 {
		t.Errorf("Storage[0].Size = %d, want 500", tmpl.Storage[0].Size)
	}
}

func TestVmResultToTemplate_NoStorage(t *testing.T) {
	t.Parallel()
	result := &vm.TemplateResult{
		InstanceType: "CPU.4V",
		StorageSize:  0,
		StorageSkip:  true,
	}

	tmpl := vmResultToTemplate(result)

	if len(tmpl.Storage) != 0 {
		t.Errorf("Storage = %v, want empty", tmpl.Storage)
	}
	if !tmpl.StorageSkip {
		t.Error("StorageSkip should be true")
	}
}
