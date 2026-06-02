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

import "testing"

// TestResolveProfile pins the precedence registry commands use to pick a
// credentials profile: explicit flag > VERDA_PROFILE > "default". (auth.profile
// from the config is covered by ActiveProfile's own tests; viper is empty in
// this unit context, so it resolves to "default".)
func TestResolveProfile(t *testing.T) {
	t.Run("no flag, no env -> default", func(t *testing.T) {
		t.Setenv("VERDA_PROFILE", "")
		if got := resolveProfile(""); got != defaultProfileName {
			t.Errorf("resolveProfile(\"\") = %q, want %q", got, defaultProfileName)
		}
	})

	t.Run("VERDA_PROFILE wins over default", func(t *testing.T) {
		t.Setenv("VERDA_PROFILE", "production")
		if got := resolveProfile(""); got != "production" {
			t.Errorf("resolveProfile(\"\") = %q, want %q", got, "production")
		}
	})

	t.Run("explicit flag wins over env", func(t *testing.T) {
		t.Setenv("VERDA_PROFILE", "production")
		if got := resolveProfile("staging"); got != "staging" {
			t.Errorf("resolveProfile(\"staging\") = %q, want %q (flag wins)", got, "staging")
		}
	})
}
