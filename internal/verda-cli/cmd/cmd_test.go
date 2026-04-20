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

package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestRegistryEnabled_EnvVar(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true}, {"true", true}, {"0", false}, {"", false}, {"yes", false},
	} {
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("VERDA_REGISTRY_ENABLED", tc.val)
			if got := registryEnabled(); got != tc.want {
				t.Fatalf("registryEnabled()=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkipCredentialResolution_RegistryChildren(t *testing.T) {
	parent := &cobra.Command{Use: "registry"}
	child := &cobra.Command{Use: "configure"}
	parent.AddCommand(child)
	if !skipCredentialResolution(child) {
		t.Error("child of `registry` should skip credential resolution")
	}
}
