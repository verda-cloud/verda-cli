package vm

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestCreateOptionsRequestDefaultsDescription(t *testing.T) {
	t.Parallel()

	opts := &createOptions{
		InstanceType: "1V100.6V",
		Image:        "ubuntu-24.04-cuda-12.8-open-docker",
		Hostname:     "gpu-runner",
		LocationCode: verda.LocationFIN01,
	}

	req, err := opts.request()
	if err != nil {
		t.Fatalf("request() returned error: %v", err)
	}

	if req.Description != opts.Hostname {
		t.Fatalf("expected description %q, got %q", opts.Hostname, req.Description)
	}
}

func TestCreateOptionsRequestBuildsOptionalFields(t *testing.T) {
	t.Parallel()

	opts := &createOptions{
		InstanceType:              "CPU.4V.16G",
		Image:                     "ubuntu-24.04",
		Hostname:                  "training-node",
		Description:               "batch worker",
		Kind:                      "cpu",
		LocationCode:              "FIN-03",
		Contract:                  "pay_as_go",
		Pricing:                   "FIXED_PRICE",
		SSHKeyIDs:                 []string{"ssh_key_1"},
		ExistingVolumes:           []string{"vol_1"},
		VolumeSpecs:               []string{"data:500:NVMe:FIN-03:move_to_trash"},
		IsSpot:                    true,
		Coupon:                    "COUPON42",
		StartupScriptID:           "script_1",
		OSVolumeSize:              100,
		OSVolumeOnSpotDiscontinue: verda.SpotDiscontinueDeletePermanent,
		StorageSize:               200,
	}

	req, err := opts.request()
	if err != nil {
		t.Fatalf("request() returned error: %v", err)
	}

	if req.StartupScriptID == nil || *req.StartupScriptID != "script_1" {
		t.Fatalf("expected startup script ID to be set")
	}
	if req.Coupon == nil || *req.Coupon != "COUPON42" {
		t.Fatalf("expected coupon to be set")
	}
	if req.Contract != "PAY_AS_YOU_GO" {
		t.Fatalf("expected contract to normalize to PAY_AS_YOU_GO, got %q", req.Contract)
	}
	if req.OSVolume == nil || req.OSVolume.Name != "training-node-os" || req.OSVolume.Size != 100 {
		t.Fatalf("expected OS volume to be populated")
	}
	if len(req.Volumes) != 2 || req.Volumes[0].OnSpotDiscontinue != verda.SpotDiscontinueMoveToTrash {
		t.Fatalf("expected parsed data volume and generated storage volume")
	}
}

func TestParseVolumeSpecRejectsSpotPolicyWithoutSpot(t *testing.T) {
	t.Parallel()

	if _, err := parseVolumeSpec("data:500:NVMe::move_to_trash", false); err == nil {
		t.Fatal("expected parseVolumeSpec to reject spot policy without --spot")
	}
}

func TestParseVolumeSpecRejectsBadFormat(t *testing.T) {
	t.Parallel()

	if _, err := parseVolumeSpec("data:not-a-size:NVMe", true); err == nil {
		t.Fatal("expected parseVolumeSpec to reject a non-numeric size")
	}
}

func TestNormalizeContractRejectsUnsupportedDurations(t *testing.T) {
	t.Parallel()

	if _, err := normalizeContract("1 year"); err == nil {
		t.Fatal("expected normalizeContract to reject long-term duration values")
	}
}
