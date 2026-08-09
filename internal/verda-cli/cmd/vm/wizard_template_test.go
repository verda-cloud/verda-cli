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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

// templateWizardMux serves the endpoints the template-mode wizard touches:
// locations for the location step. The long-term periods endpoint is
// deliberately absent — the contract loader degrades to pay-as-you-go on
// errors.
func templateWizardMux() *http.ServeMux {
	mux := baseMux()
	mux.HandleFunc("GET /locations", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"code": "FIN-01", "name": "Finland 1", "country_code": "FI"},
			{"code": "FIN-03", "name": "Finland 3", "country_code": "FI"},
		})
	})
	return mux
}

// TestTemplateWizard_DecideLaterLocationStaysEmpty is the H5 regression test:
// picking "None (decide at deploy time)" in the template wizard must leave the
// template locationless. Empty choice values trip the engine's Default
// substitution (opts.LocationCode, FIN-01), so the choice carries a sentinel.
func TestTemplateWizard_DecideLaterLocationStaysEmpty(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, templateWizardMux())
	getClient := func() (*verda.Client, error) { return h.Factory.VerdaClient() }

	opts := &createOptions{
		InstanceType:      "1V100.6V",
		Image:             "ubuntu-24.04-cuda-12.8-open-docker",
		LocationCode:      verda.LocationFIN01,
		StorageType:       verda.VolumeTypeNVMe,
		SSHKeyIDs:         []string{"key-1"},
		storageSkip:       true,
		startupScriptSkip: true,
	}

	ctx := context.Background()
	flow := buildCreateFlow(ctx, getClient, opts, WizardModeTemplate)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.SelectResult(0),                 // billing-type: On-Demand
			wizard.SelectResult(0),                 // contract: Pay as you go (periods endpoint 404s → fallback)
			wizard.SelectResult(0),                 // kind: GPU
			wizard.SelectResult(0),                 // location: None (decide at deploy time)
			wizard.TextResult("50"),                // os-volume-size
			wizard.TextResult("source-{location}"), // hostname-pattern
			wizard.TextResult(""),                  // template description
		),
	)

	if err := engine.Run(ctx, flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.LocationCode != "" {
		t.Fatalf("LocationCode = %q, want empty (decide at deploy time)", opts.LocationCode)
	}

	result := optsToTemplateResult(opts)
	if result.Location != "" {
		t.Fatalf("TemplateResult.Location = %q, want empty", result.Location)
	}

	// The saved template must not carry a location key at all.
	dir := t.TempDir()
	tmpl := &template.Template{Resource: "vm", InstanceType: result.InstanceType, Location: result.Location}
	if err := template.Save(dir, "vm", "no-location", tmpl); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "vm", "no-location.yaml"))
	if err != nil {
		t.Fatalf("reading saved template: %v", err)
	}
	if strings.Contains(string(data), "location:") {
		t.Errorf("saved template contains a location:\n%s", data)
	}
}

// TestTemplateWizard_PickedLocationPersists covers the other half of the
// sentinel fix: a real location choice is stored normally.
func TestTemplateWizard_PickedLocationPersists(t *testing.T) {
	t.Parallel()

	opts := &createOptions{LocationCode: verda.LocationFIN01}
	step := stepLocation(nil, &apiCache{}, opts, WizardModeTemplate)
	step.Setter("FIN-03")

	if opts.LocationCode != "FIN-03" {
		t.Fatalf("LocationCode = %q, want FIN-03", opts.LocationCode)
	}
}

// TestStepLocation_DecideLaterSentinelClearsLocation verifies the Setter
// translates the sentinel to "unset" instead of persisting it.
func TestStepLocation_DecideLaterSentinelClearsLocation(t *testing.T) {
	t.Parallel()

	opts := &createOptions{LocationCode: verda.LocationFIN01}
	step := stepLocation(nil, nil, opts, WizardModeTemplate)
	step.Setter(locationDecideLater)

	if opts.LocationCode != "" {
		t.Fatalf("LocationCode = %q, want empty after decide-later", opts.LocationCode)
	}
}

// TestStepLocation_ReexpandsHostnamePattern: a template hostname pattern is
// expanded at apply time against the pre-wizard location; the location step
// must re-expand {location} against the effective location so a FIN-03 deploy
// is not named ...-fin-01 (cc second-review finding).
func TestStepLocation_ReexpandsHostnamePattern(t *testing.T) {
	t.Parallel()

	tmpl := &template.Template{
		Resource:        "vm",
		InstanceType:    "1V100.6V",
		HostnamePattern: "worker-{location}",
	}
	opts := &createOptions{LocationCode: verda.LocationFIN01}
	applyTemplate(tmpl, opts, noChanged) // no location from template: expands against FIN-01 default
	if opts.Hostname != "worker-fin-01" {
		t.Fatalf("Hostname after apply = %q, want worker-fin-01", opts.Hostname)
	}

	step := stepLocation(nil, nil, opts, WizardModeDeploy)
	step.Setter("FIN-03")

	if opts.Hostname != "worker-fin-03" {
		t.Errorf("Hostname = %q, want worker-fin-03 (re-expanded against effective location)", opts.Hostname)
	}
}

// TestStepLocation_StaticHostnameNotReexpanded: patterns without {location}
// survive a location change untouched (no pointless {random} reroll, no
// stomping a manually edited hostname).
func TestStepLocation_StaticHostnameNotReexpanded(t *testing.T) {
	t.Parallel()

	opts := &createOptions{LocationCode: verda.LocationFIN01, Hostname: "my-host"}
	step := stepLocation(nil, nil, opts, WizardModeDeploy)
	step.Setter("FIN-03")

	if opts.Hostname != "my-host" {
		t.Errorf("Hostname = %q, want my-host (untouched)", opts.Hostname)
	}
}

// TestStepContract_DropsUndeployablePeriods is the H6 regression test: the
// wizard must not offer long-term periods whose codes normalizeContract
// rejects at request time.
func TestStepContract_DropsUndeployablePeriods(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	mux.HandleFunc("GET /long-term/periods/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"code": "1_month", "name": "1 month", "is_enabled": true, "discount_percentage": 5},
			{"code": "3_months", "name": "3 months", "is_enabled": true, "discount_percentage": 10},
			{"code": "1_year", "name": "1 year", "is_enabled": true, "discount_percentage": 20},
			{"code": "2_years", "name": "2 years", "is_enabled": false, "discount_percentage": 25},
		})
	})
	h := newTestHarness(t, mux)
	getClient := func() (*verda.Client, error) { return h.Factory.VerdaClient() }

	step := stepContract(getClient, &createOptions{})
	choices, err := step.Loader(context.Background(), nil, nil, wizard.NewStore())
	if err != nil {
		t.Fatalf("Loader returned error: %v", err)
	}

	if len(choices) != 1 || choices[0].Value != contractPayAsYouGo {
		t.Fatalf("expected only the pay-as-you-go choice, got %+v", choices)
	}
}

// TestStepContract_KeepsDeployableCodes: if the API ever exposes a period
// code that normalizeContract accepts, the wizard offers it.
func TestStepContract_KeepsDeployableCodes(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	mux.HandleFunc("GET /long-term/periods/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"code": "long_term", "name": "Long-term", "is_enabled": true, "discount_percentage": 10},
			{"code": "6_months", "name": "6 months", "is_enabled": true, "discount_percentage": 15},
		})
	})
	h := newTestHarness(t, mux)
	getClient := func() (*verda.Client, error) { return h.Factory.VerdaClient() }

	step := stepContract(getClient, &createOptions{})
	choices, err := step.Loader(context.Background(), nil, nil, wizard.NewStore())
	if err != nil {
		t.Fatalf("Loader returned error: %v", err)
	}

	if len(choices) != 2 || choices[1].Value != "long_term" {
		t.Fatalf("expected payg + long_term choices, got %+v", choices)
	}
}
