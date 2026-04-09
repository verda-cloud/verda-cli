package vm

import (
	"strings"
	"testing"

	"github/verda-cloud/verda-cli/internal/verda-cli/template"
)

func TestApplyTemplate(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:     "vm",
		BillingType:  "spot",
		Contract:     "PAY_AS_YOU_GO",
		Kind:         "GPU",
		InstanceType: "1V100.6V",
		Location:     "FIN-01",
		Image:        "ubuntu-24.04-cuda-12.8",
		OSVolumeSize: 200,
		Storage:      []template.StorageSpec{{Type: "NVMe", Size: 500}},
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if !opts.IsSpot {
		t.Error("expected IsSpot=true for billing_type=spot")
	}
	if opts.Contract != "PAY_AS_YOU_GO" {
		t.Errorf("Contract = %q, want PAY_AS_YOU_GO", opts.Contract)
	}
	if opts.Kind != "GPU" {
		t.Errorf("Kind = %q, want GPU", opts.Kind)
	}
	if opts.InstanceType != "1V100.6V" {
		t.Errorf("InstanceType = %q, want 1V100.6V", opts.InstanceType)
	}
	if opts.LocationCode != "FIN-01" {
		t.Errorf("LocationCode = %q, want FIN-01", opts.LocationCode)
	}
	// Image is resolved by resolveTemplateNames, not applyTemplate.
	if opts.Image != "" {
		t.Errorf("Image = %q, want empty (resolved later by resolveTemplateNames)", opts.Image)
	}
	if opts.OSVolumeSize != 200 {
		t.Errorf("OSVolumeSize = %d, want 200", opts.OSVolumeSize)
	}
	if opts.StorageSize != 500 {
		t.Errorf("StorageSize = %d, want 500", opts.StorageSize)
	}
	if opts.StorageType != "NVMe" {
		t.Errorf("StorageType = %q, want NVMe", opts.StorageType)
	}
}

func TestApplyTemplate_OnDemand(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:    "vm",
		BillingType: "on-demand",
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if opts.IsSpot {
		t.Error("expected IsSpot=false for billing_type=on-demand")
	}
}

func TestApplyTemplate_Partial(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:     "vm",
		InstanceType: "CPU.4V.16G",
		Image:        "ubuntu-24.04",
	}

	opts := &createOptions{
		LocationCode: "FIN-01", // pre-existing default
		StorageType:  "NVMe",   // pre-existing default
	}
	applyTemplate(tmpl, opts)

	if opts.InstanceType != "CPU.4V.16G" {
		t.Errorf("InstanceType = %q, want CPU.4V.16G", opts.InstanceType)
	}
	// Unset template fields should not overwrite existing defaults
	if opts.LocationCode != "FIN-01" {
		t.Errorf("LocationCode = %q, want FIN-01 (should keep default)", opts.LocationCode)
	}
	if opts.StorageType != "NVMe" {
		t.Errorf("StorageType = %q, want NVMe (should keep default)", opts.StorageType)
	}
}

func TestApplyTemplate_SkipFlags(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:          "vm",
		BillingType:       "on-demand",
		Kind:              "GPU",
		InstanceType:      "1V100.6V",
		Location:          "FIN-01",
		Image:             "ubuntu-24.04-cuda-12.8",
		OSVolumeSize:      50,
		StorageSkip:       true,
		StartupScriptSkip: true,
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if !opts.billingTypeSet {
		t.Error("expected billingTypeSet=true")
	}
	if !opts.locationSet {
		t.Error("expected locationSet=true")
	}
	if !opts.storageSkip {
		t.Error("expected storageSkip=true")
	}
	if !opts.startupScriptSkip {
		t.Error("expected startupScriptSkip=true")
	}
}

func TestApplyTemplate_HostnamePattern(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:        "vm",
		InstanceType:    "1V100.6V",
		Location:        "FIN-03",
		HostnamePattern: "gpu-{random}-{location}",
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	// Location should be applied first, then hostname pattern expanded.
	if opts.LocationCode != "FIN-03" {
		t.Errorf("LocationCode = %q, want FIN-03", opts.LocationCode)
	}

	// Hostname should start with "gpu-", end with "-fin-03", and have random words in between.
	if !strings.HasPrefix(opts.Hostname, "gpu-") {
		t.Errorf("Hostname = %q, expected prefix %q", opts.Hostname, "gpu-")
	}
	if !strings.HasSuffix(opts.Hostname, "-fin-03") {
		t.Errorf("Hostname = %q, expected suffix %q", opts.Hostname, "-fin-03")
	}
	// Should be longer than just "gpu-" + "-fin-03" = 11 chars, since {random} produces words.
	if len(opts.Hostname) <= 11 {
		t.Errorf("Hostname = %q, expected longer string with random words", opts.Hostname)
	}
}

func TestApplyTemplate_HostnamePatternNoOverwrite(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:        "vm",
		InstanceType:    "1V100.6V",
		Location:        "FIN-01",
		HostnamePattern: "gpu-{random}-{location}",
	}

	opts := &createOptions{
		Hostname: "my-existing-hostname",
	}
	applyTemplate(tmpl, opts)

	// The pre-existing hostname should NOT be overwritten by the pattern.
	if opts.Hostname != "my-existing-hostname" {
		t.Errorf("Hostname = %q, want %q (should not overwrite)", opts.Hostname, "my-existing-hostname")
	}
}

func TestApplyTemplate_HostnamePatternStaticName(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:        "vm",
		InstanceType:    "CPU.4V.16G",
		HostnamePattern: "my-worker",
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	// A pattern without placeholders should set the hostname exactly.
	if opts.Hostname != "my-worker" {
		t.Errorf("Hostname = %q, want %q", opts.Hostname, "my-worker")
	}
}

func TestApplyTemplate_WithStorage(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:     "vm",
		InstanceType: "H100x8",
		Storage:      []template.StorageSpec{{Type: "NVMe", Size: 500}},
	}

	opts := &createOptions{
		StorageType: "NVMe", // default
	}
	applyTemplate(tmpl, opts)

	if opts.StorageSize != 500 {
		t.Errorf("StorageSize = %d, want 500", opts.StorageSize)
	}
	if opts.StorageType != "NVMe" {
		t.Errorf("StorageType = %q, want NVMe", opts.StorageType)
	}
	if opts.storageSkip {
		t.Error("storageSkip should be false when storage is provided")
	}
}

func TestApplyTemplate_WithStorageHDD(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource: "vm",
		Storage:  []template.StorageSpec{{Type: "HDD", Size: 2000}},
	}

	opts := &createOptions{
		StorageType: "NVMe", // default should be overwritten
	}
	applyTemplate(tmpl, opts)

	if opts.StorageSize != 2000 {
		t.Errorf("StorageSize = %d, want 2000", opts.StorageSize)
	}
	if opts.StorageType != "HDD" {
		t.Errorf("StorageType = %q, want HDD", opts.StorageType)
	}
}

func TestApplyTemplate_StorageSkipAndStartupSkip(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:          "vm",
		InstanceType:      "A100x4",
		StorageSkip:       true,
		StartupScriptSkip: true,
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if !opts.storageSkip {
		t.Error("expected storageSkip=true")
	}
	if !opts.startupScriptSkip {
		t.Error("expected startupScriptSkip=true")
	}
}

func TestApplyTemplate_BillingTypeSetFlag(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:    "vm",
		BillingType: "on-demand",
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if !opts.billingTypeSet {
		t.Error("expected billingTypeSet=true when template has billing_type")
	}
	if opts.IsSpot {
		t.Error("expected IsSpot=false for billing_type=on-demand")
	}
}

func TestApplyTemplate_LocationSetFlag(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource: "vm",
		Location: "US-EAST-1",
	}

	opts := &createOptions{}
	applyTemplate(tmpl, opts)

	if !opts.locationSet {
		t.Error("expected locationSet=true when template has location")
	}
	if opts.LocationCode != "US-EAST-1" {
		t.Errorf("LocationCode = %q, want US-EAST-1", opts.LocationCode)
	}
}
