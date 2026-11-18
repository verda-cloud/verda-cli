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

import "testing"

// Reads only: the real API's network and subnet lists decode into the CLI's JSON shape.
func TestNetwork_ListsAreJSON(t *testing.T) {
	requireProfile(t, "test")
	for _, args := range [][]string{{"network", "list"}, {"network", "subnet", "list"}} {
		r := runAgent(t, "test", args...)
		if r.ExitCode != 0 {
			t.Skipf("%v: exit %d (private networks may be unavailable to [test]): %s", args, r.ExitCode, r.Stderr)
		}
		var rows []map[string]any
		parseJSON(t, r, &rows)
	}
}
