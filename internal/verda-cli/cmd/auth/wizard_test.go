package auth

import (
	"context"
	"testing"

	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildLoginFlowHappyPath(t *testing.T) {
	t.Parallel()

	opts := &loginOptions{
		Profile: "default",
		BaseURL: defaultBaseURL,
	}

	mock := tuitest.New()
	mock.AddTextInput("staging")                        // profile
	mock.AddTextInput("https://staging-api.verda.com")  // base-url
	mock.AddTextInput("my-id")                          // client-id
	mock.AddPassword("my-secret")                       // client-secret

	flow := buildLoginFlow(opts)
	engine := wizard.NewEngine(mock, nil)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "staging" {
		t.Errorf("expected profile=staging, got %q", opts.Profile)
	}
	if opts.BaseURL != "https://staging-api.verda.com" {
		t.Errorf("expected base-url=https://staging-api.verda.com, got %q", opts.BaseURL)
	}
	if opts.ClientID != "my-id" {
		t.Errorf("expected client-id=my-id, got %q", opts.ClientID)
	}
	if opts.ClientSecret != "my-secret" {
		t.Errorf("expected client-secret=my-secret, got %q", opts.ClientSecret)
	}
}

func TestBuildLoginFlowWithPresetFlags(t *testing.T) {
	t.Parallel()

	opts := &loginOptions{
		Profile:  "prod",
		BaseURL:  "https://custom-api.verda.com/v1",
		ClientID: "preset-id",
	}

	// Only client-secret needs prompting (profile, base-url, client-id are preset via IsSet).
	mock := tuitest.New()
	mock.AddPassword("the-secret")

	flow := buildLoginFlow(opts)
	engine := wizard.NewEngine(mock, nil)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Profile != "prod" {
		t.Errorf("expected profile=prod (preset), got %q", opts.Profile)
	}
	if opts.BaseURL != "https://custom-api.verda.com/v1" {
		t.Errorf("expected base-url preserved, got %q", opts.BaseURL)
	}
	if opts.ClientID != "preset-id" {
		t.Errorf("expected client-id=preset-id (preset), got %q", opts.ClientID)
	}
	if opts.ClientSecret != "the-secret" {
		t.Errorf("expected client-secret=the-secret, got %q", opts.ClientSecret)
	}
}
