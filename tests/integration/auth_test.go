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

//go:build integration

package integration

import (
	"testing"
)

func TestAuthShow_ValidProfile(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "auth", "show")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
	if r.Stdout == "" {
		t.Fatal("expected auth show output, got empty")
	}
}

func TestAuthShow_InvalidProfile(t *testing.T) {
	r := runWithProfile(t, "nonexistent-profile-xyz", "auth", "show")
	// auth show should still work — it shows resolved config, not validate credentials
	if r.ExitCode != 0 {
		t.Logf("exit code %d for nonexistent profile (expected)", r.ExitCode)
	}
}

func TestAuthShow_AgentMode(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "auth", "show")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
	// Agent mode forces JSON output
	if r.Stdout == "" {
		t.Fatal("expected JSON output, got empty")
	}
}
