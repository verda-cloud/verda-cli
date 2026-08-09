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

package contract

import (
	"strings"
	"testing"
	"time"
)

// TestInteractivePromptFailsCleanOnPipedStdin: a non-agent interactive path
// with piped stdin must fail fast with a clean error — the prompt UI (and any
// spinner frames) render on stderr, never stdout. Before the stream wiring,
// standalone prompts wrote ANSI to os.Stdout and polluted `verda ... | jq`.
func TestInteractivePromptFailsCleanOnPipedStdin(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	srv.SeedVolume("contract-vol", 100)

	r := runCLI(t, srv, "volume", "delete") // no --id: interactive picker path
	requireExit(t, r, 1)
	if r.Stdout != "" {
		t.Fatalf("stdout not clean on interactive path: %q", r.Stdout)
	}
	if !strings.Contains(r.Stderr, "not a TTY") {
		t.Fatalf("expected the not-a-TTY error on stderr, got: %q", r.Stderr)
	}
	if r.Duration >= 10*time.Second {
		t.Fatalf("interactive path took %s — regression toward blocking on stdin", r.Duration)
	}
}

// TestAgentUsageErrorExit2: flag misuse in agent mode is VALIDATION_ERROR with
// exit 2 (bad input), distinct from server-side failures (exit 4/1).
func TestAgentUsageErrorExit2(t *testing.T) {
	t.Parallel()
	srv := newServer(t)

	r := runCLI(t, srv, "--agent", "volume", "delete", "--status", "detached")
	requireExit(t, r, 2)
	env := parseAgentError(t, r)
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR\nstderr: %s", env.Error.Code, r.Stderr)
	}
	if !strings.Contains(env.Error.Message, "--status can only be used with --all") {
		t.Fatalf("message = %q, want the missing --all hint", env.Error.Message)
	}
	if strings.Contains(env.Error.Message, "--help") {
		t.Fatalf("message %q carries the human-facing --help hint", env.Error.Message)
	}
}

// TestAgentVMActionUsageErrorExit2: the vm action flag-combination validations
// share the same contract.
func TestAgentVMActionUsageErrorExit2(t *testing.T) {
	t.Parallel()
	srv := newServer(t)

	r := runCLI(t, srv, "--agent", "vm", "shutdown", "--status", "running")
	requireExit(t, r, 2)
	env := parseAgentError(t, r)
	if env.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR\nstderr: %s", env.Error.Code, r.Stderr)
	}
}
