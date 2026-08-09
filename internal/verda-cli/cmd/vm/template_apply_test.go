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

package vm

import (
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

// noChanged is the applyTemplate changed-predicate for "user passed no flags".
func noChanged(string) bool { return false }

// changedFlags builds a changed-predicate reporting exactly the given flags.
func changedFlags(names ...string) func(string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) bool { return set[name] }
}

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
		Hostname: "my-existing-hostname", // user passed --hostname
	}
	applyTemplate(tmpl, opts, changedFlags("hostname"))

	// The flag-passed hostname should NOT be overwritten by the pattern.
	if opts.Hostname != "my-existing-hostname" {
		t.Errorf("Hostname = %q, want %q (should not overwrite)", opts.Hostname, "my-existing-hostname")
	}
	if opts.hostnamePattern != "" {
		t.Errorf("hostnamePattern = %q, want empty (pattern not adopted)", opts.hostnamePattern)
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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

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
	applyTemplate(tmpl, opts, noChanged)

	if !opts.locationSet {
		t.Error("expected locationSet=true when template has location")
	}
	if opts.LocationCode != "US-EAST-1" {
		t.Errorf("LocationCode = %q, want US-EAST-1", opts.LocationCode)
	}
}

func TestApplyTemplate_FlagsBeatTemplate(t *testing.T) {
	t.Parallel()

	// --from gpu-training --location FIN-03 --instance-type CPU.4V.16G --os-volume-size 100
	changed := changedFlags("location", "instance-type", "os-volume-size")

	tmpl := &template.Template{
		Resource:     "vm",
		BillingType:  "on-demand",
		Kind:         "gpu",
		InstanceType: "1V100.6V",
		Location:     "FIN-01",
		OSVolumeSize: 200,
		Storage:      []template.StorageSpec{{Type: "HDD", Size: 2000}},
	}

	opts := &createOptions{
		InstanceType: "CPU.4V.16G", // from --instance-type
		LocationCode: "FIN-03",     // from --location
		OSVolumeSize: 100,          // from --os-volume-size
		StorageType:  "NVMe",       // default
	}
	applyTemplate(tmpl, opts, changed)

	if opts.LocationCode != "FIN-03" {
		t.Errorf("LocationCode = %q, want FIN-03 (flag beats template)", opts.LocationCode)
	}
	if opts.InstanceType != "CPU.4V.16G" {
		t.Errorf("InstanceType = %q, want CPU.4V.16G (flag beats template)", opts.InstanceType)
	}
	if opts.OSVolumeSize != 100 {
		t.Errorf("OSVolumeSize = %d, want 100 (flag beats template)", opts.OSVolumeSize)
	}
	// Coordination flags must not be armed for user-passed values.
	if opts.locationSet {
		t.Error("locationSet = true, want false (location came from the flag, not the template)")
	}
	// Unset fields still take template values.
	if opts.Kind != "gpu" {
		t.Errorf("Kind = %q, want gpu (unset flag takes template)", opts.Kind)
	}
	if !opts.billingTypeSet {
		t.Error("billingTypeSet = false, want true (billing came from the template)")
	}
	if opts.StorageSize != 2000 || opts.StorageType != "HDD" {
		t.Errorf("Storage = %s/%d, want HDD/2000 (unset storage flags take template)", opts.StorageType, opts.StorageSize)
	}
}

func TestApplyTemplate_BillingFlagBeatsTemplate(t *testing.T) {
	t.Parallel()

	// User passed --is-spot explicitly; template says on-demand.
	for _, flag := range []string{"is-spot", "spot"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			tmpl := &template.Template{Resource: "vm", BillingType: "on-demand"}
			opts := &createOptions{IsSpot: true}
			applyTemplate(tmpl, opts, changedFlags(flag))

			if !opts.IsSpot {
				t.Errorf("IsSpot = false, want true (--%s beats template billing_type)", flag)
			}
			if opts.billingTypeSet {
				t.Error("billingTypeSet = true, want false (billing came from the flag)")
			}
		})
	}
}

func TestApplyTemplate_StorageFlagBlocksTemplateStorage(t *testing.T) {
	t.Parallel()

	// Any explicit storage flag means the user owns storage; the template
	// must neither append its volume nor arm storageSkip.
	changed := changedFlags("storage-size")
	tmpl := &template.Template{
		Resource:    "vm",
		Storage:     []template.StorageSpec{{Type: "HDD", Size: 2000}},
		StorageSkip: true,
	}
	opts := &createOptions{StorageSize: 100, StorageType: "NVMe"}
	applyTemplate(tmpl, opts, changed)

	if opts.StorageSize != 100 || opts.StorageType != "NVMe" {
		t.Errorf("Storage = %s/%d, want NVMe/100 (flag beats template)", opts.StorageType, opts.StorageSize)
	}
	if opts.storageSkip {
		t.Error("storageSkip = true, want false (user passed storage flags)")
	}
}

func TestApplyTemplate_SentinelLocationIsNotApplied(t *testing.T) {
	t.Parallel()

	// The decide-later sentinel is wizard-internal; hand-written YAML carrying
	// it must not become a garbage location.
	tmpl := &template.Template{Resource: "vm", Location: locationDecideLater}
	opts := &createOptions{LocationCode: "FIN-01"}
	applyTemplate(tmpl, opts, noChanged)

	if opts.LocationCode != "FIN-01" {
		t.Errorf("LocationCode = %q, want FIN-01 (sentinel is not a location)", opts.LocationCode)
	}
	if opts.locationSet {
		t.Error("locationSet = true, want false (sentinel means undecided)")
	}
}

func TestApplyTemplate_HostnamePatternKeptForReExpansion(t *testing.T) {
	t.Parallel()

	// The pattern must survive apply so the wizard's location step can
	// re-expand {location} against the effective deploy location.
	tmpl := &template.Template{
		Resource:        "vm",
		Location:        "FIN-03",
		HostnamePattern: "worker-{location}",
	}
	opts := &createOptions{}
	applyTemplate(tmpl, opts, noChanged)

	if opts.hostnamePattern != "worker-{location}" {
		t.Errorf("hostnamePattern = %q, want %q", opts.hostnamePattern, "worker-{location}")
	}
	if opts.Hostname != "worker-fin-03" {
		t.Errorf("Hostname = %q, want %q", opts.Hostname, "worker-fin-03")
	}
}
