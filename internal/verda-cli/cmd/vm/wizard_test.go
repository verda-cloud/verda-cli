package vm

import (
	"context"
	"testing"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildCreateFlowHappyPath(t *testing.T) {
	t.Parallel()

	// Pre-fill API-dependent steps as if flags were provided.
	// LocationCode must differ from the default FIN-01 for IsSet to return true.
	opts := &createOptions{
		Contract:        "PAY_AS_YOU_GO",
		InstanceType:    "1V100.6V",
		LocationCode:    "FIN-03",
		Image:           "ubuntu-24.04-cuda-12.8-open-docker",
		SSHKeyIDs:       []string{"key-1"},
		StartupScriptID: "script-1",
	}

	// The wizard will prompt for: billing-type, kind, os-volume-size,
	// storage-size, hostname, description.
	mock := tuitest.New()
	mock.AddSelect(0)           // billing-type: On-Demand
	mock.AddSelect(0)           // kind: GPU
	mock.AddTextInput("100")    // os-volume-size
	mock.AddTextInput("500")    // storage-size
	mock.AddTextInput("my-gpu") // hostname
	mock.AddTextInput("")       // description (use default = hostname)

	flow := buildCreateFlow(nil, opts) // nil client OK — API steps skipped via IsSet
	engine := wizard.NewEngine(mock)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Kind != "gpu" {
		t.Errorf("expected kind=gpu, got %q", opts.Kind)
	}
	if opts.Hostname != "my-gpu" {
		t.Errorf("expected hostname=my-gpu, got %q", opts.Hostname)
	}
	if opts.OSVolumeSize != 100 {
		t.Errorf("expected os-volume-size=100, got %d", opts.OSVolumeSize)
	}
	if opts.StorageSize != 500 {
		t.Errorf("expected storage-size=500, got %d", opts.StorageSize)
	}
	if opts.IsSpot {
		t.Error("expected IsSpot=false for on-demand")
	}
	if opts.Description != "my-gpu" {
		t.Errorf("expected description=my-gpu (defaulted from hostname), got %q", opts.Description)
	}
}

func TestBuildCreateFlowSpotSkipsContract(t *testing.T) {
	t.Parallel()

	opts := &createOptions{
		InstanceType:    "1V100.6V",
		LocationCode:    "FIN-03",
		Image:           "ubuntu-24.04-cuda-12.8-open-docker",
		SSHKeyIDs:       []string{"key-1"},
		StartupScriptID: "script-1",
	}

	mock := tuitest.New()
	mock.AddSelect(1)            // billing-type: Spot Instance
	mock.AddSelect(0)            // kind: GPU
	mock.AddTextInput("50")      // os-volume-size
	mock.AddTextInput("0")       // storage-size
	mock.AddTextInput("spot-vm") // hostname
	mock.AddTextInput("")        // description

	flow := buildCreateFlow(nil, opts)
	engine := wizard.NewEngine(mock)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if !opts.IsSpot {
		t.Error("expected IsSpot=true for spot billing")
	}
	// The contract step is skipped for spot billing. The billing-type
	// setter only sets IsSpot=true. The request() method derives
	// Contract="SPOT" from IsSpot when building the API request.
	if opts.Contract != "" {
		t.Errorf("expected contract empty after wizard (derived in request()), got %q", opts.Contract)
	}
	if opts.Hostname != "spot-vm" {
		t.Errorf("expected hostname=spot-vm, got %q", opts.Hostname)
	}
}
