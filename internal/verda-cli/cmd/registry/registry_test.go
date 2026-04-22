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
	"bytes"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNewCmdRegistry_BasicWiring(t *testing.T) {
	f := cmdutil.NewTestFactory(nil)
	streams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	cmd := NewCmdRegistry(f, streams)

	if cmd.Use != "registry" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "registry")
	}
	if !cmd.Hidden {
		t.Errorf("command should be Hidden pre-GA")
	}
	if !sliceContains(cmd.Aliases, "vccr") {
		t.Errorf("alias \"vccr\" missing; got %v", cmd.Aliases)
	}
	if !sliceContains(cmd.Aliases, "vcr") {
		t.Errorf("legacy alias \"vcr\" missing; got %v", cmd.Aliases)
	}
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
