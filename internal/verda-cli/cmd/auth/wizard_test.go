package auth

import (
	"context"
	"testing"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildConfigureFlowHappyPath(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile: "default",
	}

	mock := tuitest.New()
	mock.AddTextInput("staging")  // profile
	mock.AddTextInput("my-id")    // client-id
	mock.AddPassword("my-secret") // client-secret
	mock.AddTextInput("")         // token (skip)

	flow := buildConfigureFlow(opts)
	engine := wizard.NewEngine(mock)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "staging" {
		t.Errorf("expected profile=staging, got %q", opts.Profile)
	}
	if opts.ClientID != "my-id" {
		t.Errorf("expected client-id=my-id, got %q", opts.ClientID)
	}
	if opts.ClientSecret != "my-secret" {
		t.Errorf("expected client-secret=my-secret, got %q", opts.ClientSecret)
	}
	if opts.BearerToken != "" {
		t.Errorf("expected empty token, got %q", opts.BearerToken)
	}
}

func TestBuildConfigureFlowWithPresetFlags(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile:  "prod",
		ClientID: "preset-id",
	}

	// Only client-secret and token need prompting.
	mock := tuitest.New()
	mock.AddPassword("the-secret") // client-secret
	mock.AddTextInput("tok-123")   // token

	flow := buildConfigureFlow(opts)
	engine := wizard.NewEngine(mock)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "prod" {
		t.Errorf("expected profile=prod (preset), got %q", opts.Profile)
	}
	if opts.ClientID != "preset-id" {
		t.Errorf("expected client-id=preset-id (preset), got %q", opts.ClientID)
	}
	if opts.ClientSecret != "the-secret" {
		t.Errorf("expected client-secret=the-secret, got %q", opts.ClientSecret)
	}
	if opts.BearerToken != "tok-123" {
		t.Errorf("expected token=tok-123, got %q", opts.BearerToken)
	}
}
