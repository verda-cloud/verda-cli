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

package auth

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdAuth creates the parent auth command.
func NewCmdAuth(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage shared credentials and profiles",
		Long: cmdutil.LongDesc(`
			Manage Verda shared credentials and the active auth profile.

			Paths below are defaults; VERDA_HOME moves both files. --config picks
			another config file; without it, ./config.yaml is read when
			~/.verda/config.yaml does not exist. 'verda auth use' always writes
			~/.verda/config.yaml.

			Credentials file: --auth.credentials-file, then
			VERDA_AUTH_CREDENTIALS_FILE, then auth.credentials-file in
			~/.verda/config.yaml, then VERDA_SHARED_CREDENTIALS_FILE, then
			~/.verda/credentials. 'verda auth login' and 'verda auth use' use
			their own --credentials-file, then VERDA_SHARED_CREDENTIALS_FILE,
			then ~/.verda/credentials.

			Each credential is resolved on its own, highest precedence first.
			Client ID and secret:
			  1. --auth.client-id / --auth.client-secret flags
			  2. VERDA_AUTH_CLIENT_ID / VERDA_AUTH_CLIENT_SECRET env vars
			  3. auth.client-id / auth.client-secret in ~/.verda/config.yaml
			  4. VERDA_CLIENT_ID / VERDA_CLIENT_SECRET env vars
			  5. The selected profile in the credentials file

			Bearer token: --auth.token, then VERDA_AUTH_TOKEN, then auth.token in
			config.yaml, then the selected profile. The profile only fills values
			that are still empty.

			Profile selection, highest precedence first:
			  1. --auth.profile flag
			  2. VERDA_PROFILE env var
			  3. VERDA_AUTH_PROFILE env var
			  4. auth.profile in ~/.verda/config.yaml (written by 'verda auth use')
			  5. Auto-detect: "default", or the only profile in the file

			A profile named by --auth.profile or VERDA_PROFILE is always loaded (an
			unknown name is an error when the credentials file exists), and its
			verda_base_url overrides --base-url.
			Any other profile is loaded only to fill missing values, and its
			verda_base_url applies only when --base-url is left at its default.

			This order does not apply to registry and object-storage. They keep
			their own credentials in the profile (for object-storage, VERDA_S3_*
			env vars override them field by field) and do not auto-detect a
			profile: with none set, they use "default". In registry, its own
			--profile comes first, and --auth.profile ranks below VERDA_PROFILE.
			Their credentials file is a subcommand's own --credentials-file, then
			VERDA_REGISTRY_CREDENTIALS_FILE (registry) or
			VERDA_SHARED_CREDENTIALS_FILE (object-storage), then
			~/.verda/credentials.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdLogin(f, ioStreams),
		NewCmdUse(f, ioStreams),
		NewCmdShow(f, ioStreams),
	)

	return cmd
}
