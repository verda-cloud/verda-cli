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
	"os"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// credentialsFilePath resolves the credentials file path used by registry
// subcommands.
//
// Resolution order:
//  1. flagOverride, if non-empty (e.g. --credentials-file from a subcommand)
//  2. VERDA_REGISTRY_CREDENTIALS_FILE environment variable, if non-empty
//  3. the shared default from options.DefaultCredentialsFilePath()
//
// On error resolving the default, an empty string is returned and the
// caller's loader will surface a clear error.
func credentialsFilePath(flagOverride string) string {
	if flagOverride != "" {
		return flagOverride
	}
	if p := os.Getenv("VERDA_REGISTRY_CREDENTIALS_FILE"); p != "" {
		return p
	}
	p, err := options.DefaultCredentialsFilePath()
	if err != nil {
		return ""
	}
	return p
}
