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

package vm

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// verda.Instance.CreatedAt has no omitempty, so an absent created_at would
// marshal as 0001-01-01 — a plausible date an age-based reaper acts on.
// /v1/instances populates the field today; the view removes the trap either way.
const instancesBodyNoCreatedAt = `[{"id":"inst-1","hostname":"box-a","status":"running",` +
	`"instance_type":"1V100.6V","location":"FIN-01","price_per_hour":1.23},` +
	`{"id":"inst-2","hostname":"box-b","status":"offline",` +
	`"instance_type":"1V100.6V","location":"FIN-01","price_per_hour":1.23,` +
	`"created_at":"2026-08-11T18:51:12Z"}]`

func runVMListJSON(t *testing.T, body string) string {
	t.Helper()

	mux := baseMux()
	mux.HandleFunc("GET /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	h := newTestHarness(t, mux)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdList(h.Factory, h.IOStreams))
	root.SetArgs([]string{"list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("vm list: %v", err)
	}
	return h.Stdout.String()
}

func TestListOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	out := runVMListJSON(t, instancesBodyNoCreatedAt)

	if strings.Contains(out, "0001-01-01") {
		t.Errorf("vm list emits a zero timestamp as data:\n%s", out)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2:\n%s", len(got), out)
	}
	if _, ok := got[0]["created_at"]; ok {
		t.Errorf("undated instance carries created_at: %v", got[0]["created_at"])
	}
	if got[1]["created_at"] != "2026-08-11T18:51:12Z" {
		t.Errorf("dated instance created_at = %v, want it preserved", got[1]["created_at"])
	}

	// The agent contract must survive the view mapping.
	for _, key := range []string{"id", "hostname", "status", "instance_type", "location", "price_per_hour"} {
		if _, ok := got[0][key]; !ok {
			t.Errorf("field %q dropped by the view: %v", key, got[0])
		}
	}
	if got[0]["hostname"] != "box-a" || got[0]["id"] != "inst-1" {
		t.Errorf("identity fields wrong: %v", got[0])
	}
	if got[0]["price_per_hour"] != 1.23 {
		t.Errorf("price_per_hour = %v, want 1.23 (number, not string)", got[0]["price_per_hour"])
	}
}

func TestListEmptyInstances(t *testing.T) {
	t.Parallel()

	out := runVMListJSON(t, `[]`)
	if strings.Contains(out, "0001-01-01") {
		t.Errorf("empty list emitted a timestamp:\n%s", out)
	}
}

func TestDescribeOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	mux.HandleFunc("GET /instances/inst-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"inst-1","hostname":"box-a","status":"running"}`))
	})
	h := newTestHarness(t, mux)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdDescribe(h.Factory, h.IOStreams))
	root.SetArgs([]string{"describe", "inst-1"})
	if err := root.Execute(); err != nil {
		t.Fatalf("vm describe: %v", err)
	}

	out := h.Stdout.String()
	if strings.Contains(out, "0001-01-01") {
		t.Errorf("vm describe emits a zero timestamp as data:\n%s", out)
	}
	if !strings.Contains(out, "box-a") {
		t.Errorf("describe output lost the hostname:\n%s", out)
	}
}

// The upstream 400 contradicts itself; the CLI must not relay that wording.
func TestCreateSSHKeyRequiredIsActionable(t *testing.T) {
	t.Parallel()

	mux := baseMux()
	mux.HandleFunc("POST /instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"SSH keys can be an array of UUID's, a single UUID string, null value or not defined"}`))
	})
	h := newTestHarness(t, mux)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdCreate(h.Factory, h.IOStreams))
	root.SetArgs([]string{
		"create", "--kind", "cpu", "--instance-type", "CPU.4V.16G",
		"--os", "ubuntu-24.04", "--location", "FIN-00",
		"--hostname", "box-a", "--os-volume-size", "50",
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected the create to fail")
	}

	ae := cmdutil.ClassifyError(err)
	if ae.Code != "SSH_KEY_REQUIRED" {
		t.Fatalf("code = %q, want SSH_KEY_REQUIRED (err: %v)", ae.Code, err)
	}
	if !strings.Contains(ae.Message, "--ssh-key") {
		t.Errorf("message must tell the user which flag to pass: %q", ae.Message)
	}
	if !strings.Contains(ae.Message, "ssh-key list") {
		t.Errorf("message should point at how to find ids: %q", ae.Message)
	}
	if ae.Details["api_message"] == nil {
		t.Error("the verbatim server text must survive in details for debugging")
	}
}
