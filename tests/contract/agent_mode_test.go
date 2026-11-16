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
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

// TestAgentVMListJSONPurity: in --agent mode stdout is machine-consumable
// JSON only — no spinner frames, no hint text, no ANSI — and stderr silent.
func TestAgentVMListJSONPurity(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	srv.SeedInstance("contract-alpha", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
	srv.SeedInstance("contract-beta", mockapi.TypeGPU1, mockapi.GPU1OnDemandTotal)

	r := runCLI(t, srv, "--agent", "vm", "list", "-o", "json")
	requireExit(t, r, 0)
	if r.Stderr != "" {
		t.Fatalf("stderr not empty in agent mode: %q", r.Stderr)
	}

	var instances []map[string]any
	requireCleanJSON(t, r, &instances)
	if len(instances) != 2 {
		t.Fatalf("got %d instances, want 2:\n%s", len(instances), r.Stdout)
	}
}

// TestAgentPromptBlocked: an interactive prompt in agent mode must fail fast
// with INTERACTIVE_PROMPT_BLOCKED, never block on stdin (stdin is /dev/null;
// if the safety net regresses this hangs and hits the command timeout).
func TestAgentPromptBlocked(t *testing.T) {
	t.Parallel()
	srv := newServer(t)

	r := runCLI(t, srv, "--agent", "settings", "theme")
	requireExit(t, r, 2)
	if r.Stdout != "" {
		t.Fatalf("stdout not empty on prompt-blocked path: %q", r.Stdout)
	}

	env := parseAgentError(t, r)
	if env.Error.Code != "INTERACTIVE_PROMPT_BLOCKED" {
		t.Fatalf("code = %q, want INTERACTIVE_PROMPT_BLOCKED\nstderr: %s", env.Error.Code, r.Stderr)
	}
	if got := env.Error.Details["prompt_type"]; got != "select" {
		t.Fatalf("details.prompt_type = %v, want select", got)
	}
	choices, ok := env.Error.Details["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("details.choices = %v — agents need the option list to pick a flag value", env.Error.Details["choices"])
	}
	if r.Duration >= 5*time.Second {
		t.Fatalf("prompt-blocked path took %s — regression toward blocking on stdin", r.Duration)
	}
}

// TestAgentVolumeDeleteGate: destructive actions in agent mode require --yes
// (CONFIRMATION_REQUIRED, exit 2) and must not touch state; with --yes the
// delete executes and reports a structured result.
func TestAgentVolumeDeleteGate(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	vol := srv.SeedVolume("contract-vol", 100)

	r := runCLI(t, srv, "--agent", "volume", "delete", "--id", vol.ID)
	requireExit(t, r, 2)
	env := parseAgentError(t, r)
	if env.Error.Code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q, want CONFIRMATION_REQUIRED\nstderr: %s", env.Error.Code, r.Stderr)
	}
	if got := env.Error.Details["action"]; got != "delete" {
		t.Fatalf("details.action = %v, want delete", got)
	}
	if !srv.HasVolume(vol.ID) {
		t.Fatal("volume deleted despite missing --yes; mock count:", srv.VolumeCount())
	}

	r2 := runCLI(t, srv, "--agent", "volume", "delete", "--id", vol.ID, "--yes")
	requireExit(t, r2, 0)
	var result map[string]string
	requireCleanJSON(t, r2, &result)
	if result["action"] != "delete" || result["status"] != "completed" || result["id"] != vol.ID {
		t.Fatalf("unexpected delete result: %v", result)
	}
	if srv.HasVolume(vol.ID) {
		t.Fatal("volume still present after --yes delete")
	}
}

// TestAgentVMCreateReturnsAfterIssuance: --agent vm create must not block on
// the default --wait (the flag default is locked in before --agent is parsed).
// The mock flips instances to running on first read, so a poll would be fast —
// the real assertion is wire-level: zero GET /instances/{id} unless --wait was
// passed explicitly.
func TestAgentVMCreateReturnsAfterIssuance(t *testing.T) {
	t.Parallel()
	srv := newServer(t)

	r := runCLI(t, srv, "--agent", "vm", "create",
		"--kind", "cpu",
		"--instance-type", mockapi.TypeCPU,
		"--os", "ubuntu-24.04",
		"--hostname", "contract-nowait",
	)
	requireExit(t, r, 0)
	var inst struct {
		ID string `json:"id"`
	}
	requireCleanJSON(t, r, &inst)
	if inst.ID == "" {
		t.Fatalf("create returned empty instance id:\n%s", r.Stdout)
	}
	if n := srv.InstanceGetCount(); n != 0 {
		t.Fatalf("default --wait polled instance status %d times in agent mode; want 0 (agents poll via vm describe)", n)
	}
	if r.Duration >= 5*time.Second {
		t.Fatalf("issuance-only create took %s — regression toward blocking", r.Duration)
	}

	r2 := runCLI(t, srv, "--agent", "vm", "create",
		"--kind", "cpu",
		"--instance-type", mockapi.TypeCPU,
		"--os", "ubuntu-24.04",
		"--hostname", "contract-wait",
		"--wait",
	)
	requireExit(t, r2, 0)
	if n := srv.InstanceGetCount(); n == 0 {
		t.Fatal("explicit --wait in agent mode did not poll instance status")
	}
}

// TestAgentErrorClassification: HTTP status codes map to the documented
// error codes and exit codes (docs/agent-errors.md).
func TestAgentErrorClassification(t *testing.T) {
	t.Parallel()

	t.Run("401 maps to AUTH_ERROR exit 3", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		srv.FailRoute("/instances", 401)

		r := runCLI(t, srv, "--agent", "vm", "list")
		requireExit(t, r, 3)
		env := parseAgentError(t, r)
		if env.Error.Code != "AUTH_ERROR" {
			t.Fatalf("code = %q, want AUTH_ERROR\nstderr: %s", env.Error.Code, r.Stderr)
		}
		if got := env.Error.Details["status"]; got != float64(401) {
			t.Fatalf("details.status = %v, want 401", got)
		}
	})

	t.Run("500 maps to API_ERROR exit 4", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		srv.FailRoute("/instances", 500)

		r := runCLI(t, srv, "--agent", "vm", "list")
		requireExit(t, r, 4)
		env := parseAgentError(t, r)
		if env.Error.Code != "API_ERROR" {
			t.Fatalf("code = %q, want API_ERROR\nstderr: %s", env.Error.Code, r.Stderr)
		}
		if got := env.Error.Details["status"]; got != float64(500) {
			t.Fatalf("details.status = %v, want 500", got)
		}
	})
}

func TestAgentNetworkCommands(t *testing.T) {
	t.Parallel()

	t.Run("list and subnet list are clean JSON", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		n := srv.SeedNetwork("contract-net", true)
		srv.SeedSubnet(n.ID, "fin-03-1", "FIN-03", "10.0.0.0/24")

		for _, args := range [][]string{{"network", "list"}, {"network", "subnet", "list"}, {"network", "describe", n.ID}} {
			r := runCLI(t, srv, append([]string{"--agent"}, args...)...)
			requireExit(t, r, 0)
			if r.Stderr != "" {
				t.Fatalf("%v: stderr not empty in agent mode: %q", args, r.Stderr)
			}
			var v any
			requireCleanJSON(t, r, &v)
		}
	})

	t.Run("delete needs --yes, then deletes", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		n := srv.SeedNetwork("contract-del", false)

		r := runCLI(t, srv, "--agent", "network", "delete", n.ID)
		requireExit(t, r, 2)
		if env := parseAgentError(t, r); env.Error.Code != "CONFIRMATION_REQUIRED" {
			t.Fatalf("code = %q, want CONFIRMATION_REQUIRED", env.Error.Code)
		}
		if !srv.HasNetwork(n.ID) {
			t.Fatal("network deleted without --yes")
		}

		r = runCLI(t, srv, "--agent", "network", "delete", n.ID, "--yes")
		requireExit(t, r, 0)
		var result map[string]string
		requireCleanJSON(t, r, &result)
		if result["status"] != "completed" || result["id"] != n.ID || srv.HasNetwork(n.ID) {
			t.Fatalf("result = %v, still present = %v", result, srv.HasNetwork(n.ID))
		}
	})

	t.Run("delete with instances is RESOURCE_IN_USE", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		n := srv.SeedNetwork("contract-busy", false)
		sub := srv.SeedSubnet(n.ID, "fin-03-1", "FIN-03", "10.0.0.0/24")
		inst := srv.SeedInstance("contract-vm", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
		srv.AttachInstance(inst.ID, n.ID, sub.ID)

		r := runCLI(t, srv, "--agent", "network", "delete", n.ID, "--yes")
		requireExit(t, r, 1)
		env := parseAgentError(t, r)
		if env.Error.Code != "RESOURCE_IN_USE" || !srv.HasNetwork(n.ID) {
			t.Fatalf("code = %q, still present = %v\nstderr: %s", env.Error.Code, srv.HasNetwork(n.ID), r.Stderr)
		}
		if blockers, _ := env.Error.Details["blockers"].([]any); len(blockers) != 1 {
			t.Fatalf("details.blockers = %v", env.Error.Details["blockers"])
		}
	})

	t.Run("409 keeps API_ERROR and adds api_code", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		n := srv.SeedNetwork("contract-409", false)
		srv.FailRoute("/private-networks/"+n.ID, 409)

		r := runCLI(t, srv, "--agent", "network", "delete", n.ID, "--yes")
		requireExit(t, r, 4)
		env := parseAgentError(t, r)
		if env.Error.Code != "API_ERROR" || env.Error.Details["api_code"] == nil || env.Error.Details["status"] != float64(409) {
			t.Fatalf("error = %+v", env.Error)
		}
	})

	t.Run("describe of a missing network is NOT_FOUND", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		r := runCLI(t, srv, "--agent", "network", "describe", "pn_missing")
		requireExit(t, r, 5)
		if env := parseAgentError(t, r); env.Error.Code != "NOT_FOUND" {
			t.Fatalf("code = %q, want NOT_FOUND", env.Error.Code)
		}
	})

	t.Run("create without --name is MISSING_REQUIRED_FLAGS", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		r := runCLI(t, srv, "--agent", "network", "create")
		requireExit(t, r, 2)
		if env := parseAgentError(t, r); env.Error.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("code = %q, want MISSING_REQUIRED_FLAGS", env.Error.Code)
		}
	})

	t.Run("subnet create picks a CIDR", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		n := srv.SeedNetwork("contract-sub", true)
		srv.SeedSubnet(n.ID, "fin-03-1", "FIN-03", "10.0.0.0/24")

		r := runCLI(t, srv, "--agent", "network", "subnet", "create", "--network", n.Name, "-l", "FIN-03")
		requireExit(t, r, 0)
		var sub map[string]any
		requireCleanJSON(t, r, &sub)
		if sub["cidr"] != "10.0.1.0/24" || sub["name"] != "fin-03-2" {
			t.Fatalf("subnet = %v", sub)
		}
	})
}
