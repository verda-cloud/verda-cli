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

package auth

import (
	"context"
	"io"
	"testing"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildLoginFlowHappyPath(t *testing.T) {
	t.Parallel()

	opts := &loginOptions{
		Profile: "default",
		BaseURL: defaultBaseURL,
	}

	flow := buildLoginFlow(opts)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.TextResult("staging"),                       // profile
			wizard.TextResult("https://staging-api.verda.com"), // base-url
			wizard.TextResult("my-id"),                         // client-id
			wizard.TextResult("my-secret"),                     // client-secret (password prompt returns text too)
		),
	)

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
	flow := buildLoginFlow(opts)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.TextResult("the-secret"), // client-secret
		),
	)

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
