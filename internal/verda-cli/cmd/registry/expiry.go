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
	"fmt"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// checkExpiry is a pre-flight helper that registry commands (push, copy, ls,
// tags, login) call BEFORE dialing the registry. It returns a structured
// AgentError when credentials are missing or demonstrably expired so the CLI
// fails with an actionable message instead of a generic auth error from ggcr.
//
// Behavior:
//   - nil or !HasCredentials()      → registry_not_configured
//   - ExpiresAt.IsZero()            → nil (legacy / non-expiring row; server
//     is authoritative — let the request proceed)
//   - IsExpired()                   → registry_credential_expired
//   - otherwise                     → nil
//
// Pure: no network, no file I/O, no factory access.
func checkExpiry(creds *options.RegistryCredentials) error {
	if creds == nil || !creds.HasCredentials() {
		return &cmdutil.AgentError{
			Code:     kindRegistryNotConfigured,
			Message:  "Registry is not configured. Run `verda registry configure` first.",
			ExitCode: cmdutil.ExitAuth,
		}
	}

	// Legacy / non-expiring rows: let the server be the source of truth.
	if creds.ExpiresAt.IsZero() {
		return nil
	}

	if creds.IsExpired() {
		expires := creds.ExpiresAt.UTC().Format("2006-01-02")
		return &cmdutil.AgentError{
			Code: kindRegistryCredentialExpired,
			Message: fmt.Sprintf(
				"Registry credential expired on %s. Create a new credential in the Verda UI, then run `verda registry configure`.",
				expires,
			),
			Details: map[string]any{
				"expires_at": expires,
			},
			ExitCode: cmdutil.ExitAuth,
		}
	}

	return nil
}
