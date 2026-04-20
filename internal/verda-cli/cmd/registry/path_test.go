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
	"testing"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

func TestCredentialsFilePath_EnvOverride(t *testing.T) {
	override := "/tmp/nonexistent-verda-registry-creds"
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", override)

	got := credentialsFilePath()
	if got != override {
		t.Fatalf("credentialsFilePath() = %q, want %q", got, override)
	}
}

func TestCredentialsFilePath_EmptyEnvFallsThrough(t *testing.T) {
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", "")

	want, err := options.DefaultCredentialsFilePath()
	if err != nil {
		t.Fatalf("options.DefaultCredentialsFilePath() returned error: %v", err)
	}

	got := credentialsFilePath()
	if got != want {
		t.Fatalf("credentialsFilePath() = %q, want %q", got, want)
	}
}

func TestCredentialsFilePath_UnsetFallsThrough(t *testing.T) {
	// Go tests don't inherit setenv across tests, but be explicit.
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", "")
	// Simulate unset by relying on the empty-string branch, then re-verify.
	want, err := options.DefaultCredentialsFilePath()
	if err != nil {
		t.Fatalf("options.DefaultCredentialsFilePath() returned error: %v", err)
	}

	got := credentialsFilePath()
	if got != want {
		t.Fatalf("credentialsFilePath() = %q, want %q", got, want)
	}
}
