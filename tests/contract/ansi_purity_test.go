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

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

// Escapes reach a pipe whenever a command styles its own output: lipgloss
// renders them unconditionally, and only the writer knows the destination. This
// suite runs the real binary with stdout piped — the shape every `verda … | jq`
// and `> file` takes — across the commands that render tables and cards.
//
// Per-command tests cannot catch this class: they hand commands a raw
// bytes.Buffer, so they never exercise the wiring that decides on color.
func TestPipedOutputCarriesNoANSI(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		seed func(*mockapi.Server)
	}{
		{name: "instance-types", args: []string{"instance-types"}},
		{name: "instance-types --cpu", args: []string{"instance-types", "--cpu"}},
		{name: "availability", args: []string{"availability"}},
		{name: "locations", args: []string{"locations"}},
		{name: "cost balance", args: []string{"cost", "balance"}},
		{name: "cost estimate", args: []string{"cost", "estimate", "--type", mockapi.TypeCPU}},
		{
			name: "cost running",
			args: []string{"cost", "running"},
			seed: func(s *mockapi.Server) {
				s.SeedInstance("ansi-burn", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
			},
		},
		{
			name: "vm list",
			args: []string{"vm", "list"},
			seed: func(s *mockapi.Server) {
				s.SeedInstance("ansi-alpha", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
			},
		},
		{
			name: "volume list",
			args: []string{"volume", "list"},
			seed: func(s *mockapi.Server) { s.SeedVolume("ansi-vol", 100) },
		},
		{name: "ssh-key list", args: []string{"ssh-key", "list"}},
		{name: "status", args: []string{"status"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newServer(t)
			if tc.seed != nil {
				tc.seed(srv)
			}

			r := runCLI(t, srv, tc.args...)

			if strings.ContainsRune(r.Stdout, '\033') {
				t.Errorf("stdout carries ANSI escapes when piped:\n%q", r.Stdout)
			}
			// Guard against passing for the wrong reason: an empty or failed run
			// has no escapes either.
			if r.ExitCode != 0 {
				t.Errorf("exit = %d, want 0\nstderr: %s", r.ExitCode, r.Stderr)
			}
			if strings.TrimSpace(r.Stdout) == "" {
				t.Errorf("no stdout to inspect; the assertion would be vacuous\nstderr: %s", r.Stderr)
			}
		})
	}
}

// Structured output must be parseable byte-for-byte, so the same rule applies
// with -o json — and here an escape would break json.Unmarshal outright.
func TestPipedJSONCarriesNoANSI(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	srv.SeedInstance("ansi-json", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)
	srv.SeedVolume("ansi-json-vol", 50)

	for _, args := range [][]string{
		{"vm", "list", "-o", "json"},
		{"volume", "list", "-o", "json"},
		{"instance-types", "-o", "json"},
		{"cost", "running", "-o", "json"},
	} {
		name := strings.Join(args, " ")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := runCLI(t, srv, args...)
			if r.ExitCode != 0 {
				t.Fatalf("exit = %d\nstderr: %s", r.ExitCode, r.Stderr)
			}
			if strings.ContainsRune(r.Stdout, '\033') {
				t.Errorf("JSON stdout carries ANSI escapes:\n%q", r.Stdout)
			}
			if !strings.HasPrefix(strings.TrimSpace(r.Stdout), "[") &&
				!strings.HasPrefix(strings.TrimSpace(r.Stdout), "{") {
				t.Errorf("stdout is not JSON: %q", r.Stdout)
			}
		})
	}
}
