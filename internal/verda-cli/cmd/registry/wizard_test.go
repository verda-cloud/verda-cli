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

package registry

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildConfigureFlow_Structure(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile:       defaultProfileName,
		ExpiresInDays: defaultExpiresInDays,
	}
	flow := buildConfigureFlow(opts)
	if flow == nil {
		t.Fatal("buildConfigureFlow returned nil")
	}
	if len(flow.Steps) != 3 {
		t.Fatalf("flow has %d steps, want 3", len(flow.Steps))
	}
	wantNames := []string{"paste", "expires-in", "docker-config"}
	for i, want := range wantNames {
		if flow.Steps[i].Name != want {
			t.Errorf("step %d: got %q, want %q", i, flow.Steps[i].Name, want)
		}
	}
}

func TestBuildConfigureFlow_PasteValidator(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{ExpiresInDays: defaultExpiresInDays}
	flow := buildConfigureFlow(opts)
	paste := flow.Steps[0]

	if paste.Validate == nil {
		t.Fatal("paste step has no Validate func")
	}
	// Accepts a valid docker login string.
	if err := paste.Validate("docker login -u vcr-abc+cli -p s3cret vccr.io"); err != nil {
		t.Errorf("valid paste rejected: %v", err)
	}
	// Rejects empty input with a clear error.
	if err := paste.Validate(""); err == nil {
		t.Error("empty paste accepted; want error")
	}
	// Rejects a non-docker-login line, surfacing parseDockerLogin's diagnostic.
	if err := paste.Validate("hello world"); err == nil {
		t.Error("garbage paste accepted; want error")
	}
	// Rejects a username missing the vcr- prefix.
	if err := paste.Validate("docker login -u nope+cli -p s3cret vccr.io"); err == nil {
		t.Error("malformed username accepted; want error")
	}
}

func TestBuildConfigureFlow_ExpiresInValidator(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{ExpiresInDays: defaultExpiresInDays}
	flow := buildConfigureFlow(opts)
	step := flow.Steps[1]

	if step.Validate == nil {
		t.Fatal("expires-in step has no Validate func")
	}
	// Valid non-negative ints.
	for _, v := range []string{"0", "7", "30", "365"} {
		if err := step.Validate(v); err != nil {
			t.Errorf("valid %q rejected: %v", v, err)
		}
	}
	// Negative rejected.
	if err := step.Validate("-1"); err == nil {
		t.Error("negative accepted; want error")
	}
	// Non-integer rejected.
	if err := step.Validate("seven"); err == nil {
		t.Error("non-integer accepted; want error")
	}
	// Empty rejected.
	if err := step.Validate(""); err == nil {
		t.Error("empty accepted; want error")
	}

	// Default provides the documented default.
	if step.Default == nil {
		t.Fatal("expires-in step has no Default func")
	}
	if got := step.Default(map[string]any{}); got != "30" {
		t.Errorf("default = %v, want %q", got, "30")
	}
}

func TestBuildConfigureFlow_DockerConfigDefaultsYes(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{ExpiresInDays: defaultExpiresInDays}
	flow := buildConfigureFlow(opts)
	step := flow.Steps[2]

	if step.Default == nil {
		t.Fatal("docker-config step has no Default func")
	}
	got, ok := step.Default(map[string]any{}).(bool)
	if !ok {
		t.Fatalf("default type = %T, want bool", step.Default(map[string]any{}))
	}
	if !got {
		t.Error("docker-config default = false, want true (Y)")
	}
}

// TestBuildConfigureFlow_HappyPath drives the full 3-step flow through the
// wizard engine in test mode and asserts opts is populated as the flag-path
// expects. Mirrors s3/wizard_test.go TestBuildConfigureFlowHappyPath.
func TestBuildConfigureFlow_HappyPath(t *testing.T) {
	t.Parallel()

	opts := &configureOptions{
		Profile:       defaultProfileName,
		ExpiresInDays: defaultExpiresInDays,
	}
	flow := buildConfigureFlow(opts)
	engine := wizard.NewEngine(nil, nil,
		wizard.WithOutput(io.Discard),
		wizard.WithTestResults(
			wizard.TextResult("docker login -u vcr-abc+cli -p s3cret vccr.io"), // paste
			wizard.TextResult("7"),     // expires-in
			wizard.ConfirmResult(true), // docker-config
		),
	)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Username != "vcr-abc+cli" {
		t.Errorf("Username = %q, want %q", opts.Username, "vcr-abc+cli")
	}
	if opts.Endpoint != "vccr.io" {
		t.Errorf("Endpoint = %q, want %q", opts.Endpoint, "vccr.io")
	}
	if !strings.HasPrefix(opts.Paste, "docker login") {
		t.Errorf("Paste not stored: %q", opts.Paste)
	}
	if opts.ExpiresInDays != 7 {
		t.Errorf("ExpiresInDays = %d, want 7", opts.ExpiresInDays)
	}
	if !opts.DockerConfig {
		t.Error("DockerConfig = false, want true")
	}
}
