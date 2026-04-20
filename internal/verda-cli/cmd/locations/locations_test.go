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

package locations

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestLocationsStructuredOutputJSON(t *testing.T) {
	t.Parallel()

	locations := []verda.Location{
		{Code: "FIN-01", Name: "Finland 1", CountryCode: "FI"},
		{Code: "FIN-03", Name: "Finland 3", CountryCode: "FI"},
	}

	var buf bytes.Buffer
	wrote, err := cmdutil.WriteStructured(&buf, "json", locations)
	if err != nil {
		t.Fatalf("WriteStructured error: %v", err)
	}
	if !wrote {
		t.Fatal("expected structured output to be written")
	}

	var decoded []verda.Location
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(decoded))
	}
	if decoded[0].Code != "FIN-01" || decoded[1].Code != "FIN-03" {
		t.Fatalf("unexpected locations: %v", decoded)
	}
}

func TestLocationsStructuredOutputYAML(t *testing.T) {
	t.Parallel()

	locations := []verda.Location{
		{Code: "FIN-01", Name: "Finland 1", CountryCode: "FI"},
	}

	var buf bytes.Buffer
	wrote, err := cmdutil.WriteStructured(&buf, "yaml", locations)
	if err != nil {
		t.Fatalf("WriteStructured error: %v", err)
	}
	if !wrote {
		t.Fatal("expected structured output to be written")
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty YAML output")
	}
}

func TestLocationsCommandAliases(t *testing.T) {
	t.Parallel()

	cmd := NewCmdLocations(nil, cmdutil.IOStreams{})
	if cmd.Use != "locations" {
		t.Fatalf("expected Use 'locations', got %q", cmd.Use)
	}

	aliases := map[string]bool{}
	for _, a := range cmd.Aliases {
		aliases[a] = true
	}
	for _, expected := range []string{"location", "loc"} {
		if !aliases[expected] {
			t.Errorf("missing alias %q", expected)
		}
	}
}
