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
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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

// ---------------------------------------------------------------------------
// Orchestration tests for runCreate
// ---------------------------------------------------------------------------

// TestRunCreate_AgentMode_MissingFlags verifies that runCreate returns a
// MissingFlagsError when required flags are omitted in agent mode.
func TestRunCreate_AgentMode_MissingFlags(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	h := newTestHarness(t, mux)

	cmd := NewCmdCreate(h.Factory, h.IOStreams)
	// Provide only --instance-type and --os, omit --hostname and --kind.
	cmd.SetArgs([]string{
		"--instance-type", "1V100.6V",
		"--os", "ubuntu-24.04-cuda-12.8-open-docker",
		"--wait=false",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for missing flags, got nil")
	}

	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}

	if agentErr.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("expected code MISSING_REQUIRED_FLAGS, got %q", agentErr.Code)
	}

	missing, ok := agentErr.Details["missing"].([]string)
	if !ok {
		t.Fatalf("expected missing details to be []string, got %T", agentErr.Details["missing"])
	}

	missingSet := make(map[string]bool, len(missing))
	for _, m := range missing {
		missingSet[m] = true
	}
	if !missingSet["--kind"] {
		t.Error("expected --kind in missing flags")
	}
	if !missingSet["--hostname"] {
		t.Error("expected --hostname in missing flags")
	}
	if missingSet["--instance-type"] {
		t.Error("--instance-type was provided but listed as missing")
	}
	if missingSet["--os"] {
		t.Error("--os was provided but listed as missing")
	}
}

// TestRunCreate_AgentMode_AllFlags verifies the full runCreate flow in agent
// mode with all required flags, a mock API server, and JSON output.
func TestRunCreate_AgentMode_AllFlags(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	mux.HandleFunc("POST /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "inst-001",
			"hostname":       "gpu-runner",
			"status":         "new",
			"instance_type":  "1V100.6V",
			"image":          "ubuntu-24.04-cuda-12.8-open-docker",
			"location":       "FIN-01",
			"price_per_hour": 1.50,
		})
	})

	h := newTestHarness(t, mux)

	cmd := NewCmdCreate(h.Factory, h.IOStreams)
	cmd.SetArgs([]string{
		"--kind", "gpu",
		"--instance-type", "1V100.6V",
		"--os", "ubuntu-24.04-cuda-12.8-open-docker",
		"--hostname", "gpu-runner",
		"--location", "FIN-01",
		"--wait=false",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v", err)
	}

	// Parse JSON output
	var result map[string]any
	if err := json.Unmarshal(h.Stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, h.Stdout.String())
	}

	if got := result["id"]; got != "inst-001" {
		t.Errorf("expected id=inst-001, got %v", got)
	}
	if got := result["hostname"]; got != "gpu-runner" {
		t.Errorf("expected hostname=gpu-runner, got %v", got)
	}
}

// TestRunCreate_AgentMode_WithTemplate verifies that --from loads a template
// file and its values appear in the API request. Required flags are still
// provided on the CLI because the missing-flags check runs before template
// application (the template can override values via resolveCreateInputs).
func TestRunCreate_AgentMode_WithTemplate(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	var capturedReq map[string]any
	mux.HandleFunc("POST /instances", func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body for assertions.
		_ = json.NewDecoder(r.Body).Decode(&capturedReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "inst-tmpl-001",
			"hostname":       "from-template",
			"status":         "new",
			"instance_type":  "1V100.6V",
			"image":          "ubuntu-24.04-cuda-12.8-open-docker",
			"location":       "FIN-03",
			"price_per_hour": 1.50,
		})
	})

	h := newTestHarness(t, mux)

	// Create a template YAML in a temp directory.
	tmplDir := t.TempDir()
	tmplContent := `resource: vm
kind: gpu
instance_type: 1V100.6V
location: FIN-03
image: ubuntu-24.04-cuda-12.8-open-docker
hostname_pattern: from-template
`
	tmplPath := filepath.Join(tmplDir, "test-template.yaml")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o600); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	cmd := NewCmdCreate(h.Factory, h.IOStreams)
	// Required flags must be passed because missingCreateFlags is checked
	// before the template is applied. The template overrides location.
	cmd.SetArgs([]string{
		"--from", tmplPath,
		"--kind", "gpu",
		"--instance-type", "1V100.6V",
		"--os", "ubuntu-24.04-cuda-12.8-open-docker",
		"--hostname", "from-template",
		"--wait=false",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, h.Stderr.String())
	}

	// Verify JSON output
	var result map[string]any
	if err := json.Unmarshal(h.Stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, h.Stdout.String())
	}

	if got := result["id"]; got != "inst-tmpl-001" {
		t.Errorf("expected id=inst-tmpl-001, got %v", got)
	}

	// Verify the API received the template-overridden location (FIN-03).
	if capturedReq == nil {
		t.Fatal("expected API request to be captured")
	}
	if got := capturedReq["location_code"]; got != "FIN-03" {
		t.Errorf("expected location_code=FIN-03 (from template) in API request, got %v", got)
	}
	if got := capturedReq["hostname"]; got != "from-template" {
		t.Errorf("expected hostname=from-template in API request, got %v", got)
	}
}

// TestRunCreate_AgentMode_TemplateMissingFlags verifies that in agent mode,
// --from alone is not sufficient when the template would provide required
// values -- the missing-flags check fires before template application.
func TestRunCreate_AgentMode_TemplateMissingFlags(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	h := newTestHarness(t, mux)

	tmplDir := t.TempDir()
	tmplContent := `resource: vm
kind: gpu
instance_type: 1V100.6V
location: FIN-03
image: ubuntu-24.04-cuda-12.8-open-docker
hostname_pattern: from-template
`
	tmplPath := filepath.Join(tmplDir, "test-template.yaml")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o600); err != nil {
		t.Fatalf("failed to write template file: %v", err)
	}

	cmd := NewCmdCreate(h.Factory, h.IOStreams)
	// Only --from is provided; the template would fill all values, but the
	// missing-flags check runs before template application.
	cmd.SetArgs([]string{
		"--from", tmplPath,
		"--wait=false",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected MissingFlagsError, got nil")
	}

	if !cmdutil.IsAgentError(err) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
}

// TestRunCreate_AgentMode_NoClient verifies that runCreate returns an error
// when no credentials are configured.
func TestRunCreate_AgentMode_NoClient(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	f := &cmdutil.TestFactory{
		AgentModeOverride:    true,
		OutputFormatOverride: "json",
	}
	ioStreams := cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr}

	cmd := NewCmdCreate(f, ioStreams)
	cmd.SetArgs([]string{
		"--kind", "gpu",
		"--instance-type", "1V100.6V",
		"--os", "ubuntu-24.04",
		"--hostname", "test-vm",
		"--wait=false",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no client is configured, got nil")
	}

	// The error should be from VerdaClient() returning ErrNoClient.
	if err.Error() != cmdutil.ErrNoClient.Error() {
		t.Errorf("expected ErrNoClient error, got: %v", err)
	}
}
