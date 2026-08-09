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

// TestTableStreamSeparation: table mode writes data to stdout and never
// interleaves diagnostics on stderr (stdout is a pipe here — the piped case).
func TestTableStreamSeparation(t *testing.T) {
	t.Parallel()
	srv := newServer(t)
	srv.SeedInstance("contract-table", mockapi.TypeCPU, mockapi.CPUOnDemandTotal)

	r := runCLI(t, srv, "vm", "list")
	requireExit(t, r, 0)
	if r.Stderr != "" {
		t.Fatalf("stderr not empty in table mode with piped stdout: %q", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "HOSTNAME") || !strings.Contains(r.Stdout, "contract-table") {
		t.Fatalf("stdout does not contain the instance table:\n%s", r.Stdout)
	}
}

// TestPricePerHourIsTotal (review C1): the API's instance price_per_hour is
// the TOTAL hourly price for the type. The mock catalog prices the 8-GPU type
// at exactly 8x its 1-GPU sibling; if any CLI layer multiplies the wire value
// by unit count again, these exact-equality checks break.
func TestPricePerHourIsTotal(t *testing.T) {
	t.Parallel()

	create := func(t *testing.T, srv *mockapi.Server, args ...string) (id string, price float64) {
		t.Helper()
		r := runCLI(t, srv, args...)
		requireExit(t, r, 0)
		var inst struct {
			ID           string  `json:"id"`
			PricePerHour float64 `json:"price_per_hour"`
		}
		requireCleanJSON(t, r, &inst)
		if inst.ID == "" {
			t.Fatalf("create returned empty instance id:\n%s", r.Stdout)
		}
		return inst.ID, inst.PricePerHour
	}

	describe := func(t *testing.T, srv *mockapi.Server, id string) float64 {
		t.Helper()
		r := runCLI(t, srv, "--agent", "vm", "describe", id)
		requireExit(t, r, 0)
		var inst struct {
			PricePerHour float64 `json:"price_per_hour"`
		}
		requireCleanJSON(t, r, &inst)
		return inst.PricePerHour
	}

	t.Run("8-GPU create/describe/list carry the catalog total untouched", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)

		id, createPrice := create(t, srv,
			"--agent", "vm", "create",
			"--kind", "gpu",
			"--instance-type", mockapi.TypeGPU8,
			"--os", "ubuntu-24.04",
			"--hostname", "c1-gpu-rig",
		)
		if createPrice != mockapi.GPU8OnDemandTotal {
			t.Fatalf("create price_per_hour = %v, want catalog total %v (8x %v; a re-multiplied wire value would read %v)",
				createPrice, mockapi.GPU8OnDemandTotal, mockapi.GPU1OnDemandTotal, mockapi.GPU8OnDemandTotal*8)
		}
		if got := describe(t, srv, id); got != mockapi.GPU8OnDemandTotal {
			t.Fatalf("describe price_per_hour = %v, want catalog total %v", got, mockapi.GPU8OnDemandTotal)
		}

		r := runCLI(t, srv, "--agent", "vm", "list")
		requireExit(t, r, 0)
		var insts []struct {
			InstanceType string  `json:"instance_type"`
			PricePerHour float64 `json:"price_per_hour"`
		}
		requireCleanJSON(t, r, &insts)
		found := false
		for i := range insts {
			if insts[i].InstanceType == mockapi.TypeGPU8 {
				found = true
				if insts[i].PricePerHour != mockapi.GPU8OnDemandTotal {
					t.Fatalf("list price_per_hour = %v, want catalog total %v", insts[i].PricePerHour, mockapi.GPU8OnDemandTotal)
				}
			}
		}
		if !found {
			t.Fatalf("created instance missing from list:\n%s", r.Stdout)
		}
	})

	// On-demand and spot totals pinned to the staging ground-truth values in
	// temp/docs/c1-ondemand-instance.json (CPU.4V.16G: 0.0279 / 0.0098).
	t.Run("CPU on-demand and spot totals match staging ground truth", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)

		_, onDemand := create(t, srv,
			"--agent", "vm", "create",
			"--kind", "cpu",
			"--instance-type", mockapi.TypeCPU,
			"--os", "ubuntu-24.04",
			"--hostname", "c1-cpu-ondemand",
		)
		if onDemand != mockapi.CPUOnDemandTotal {
			t.Fatalf("on-demand price_per_hour = %v, want %v", onDemand, mockapi.CPUOnDemandTotal)
		}

		_, spot := create(t, srv,
			"--agent", "vm", "create",
			"--kind", "cpu",
			"--instance-type", mockapi.TypeCPU,
			"--os", "ubuntu-24.04",
			"--hostname", "c1-cpu-spot",
			"--is-spot",
		)
		if spot != mockapi.CPUSpotTotal {
			t.Fatalf("spot price_per_hour = %v, want %v", spot, mockapi.CPUSpotTotal)
		}
	})
}
