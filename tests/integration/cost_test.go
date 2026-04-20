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

func TestCostBalance(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "balance")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var balance map[string]any
	parseJSON(t, r, &balance)

	if _, ok := balance["amount"]; !ok {
		t.Error("balance missing 'amount' field")
	}
}

func TestCostEstimate(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "1A6000.10V")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostEstimate_WithStorage(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "1A6000.10V", "--os-volume", "100", "--storage", "500")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostRunning(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "running")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostEstimate_InvalidType(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "NONEXISTENT.TYPE")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid instance type")
	}
}
