package vm

import (
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
	if opts.Image != "ubuntu-24.04-cuda-12.8" {
		t.Errorf("Image = %q, want ubuntu-24.04-cuda-12.8", opts.Image)
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
