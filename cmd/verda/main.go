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

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	cmd "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func main() {
	root, opts := cmd.NewRootCommand(cmdutil.NewStdIOStreams())
	if err := root.Execute(); errors.Is(err, cmd.ErrVersionRequested) {
		// --version flag was handled; exit cleanly.
		return
	} else if err != nil {
		// In agent mode, always emit structured JSON errors.
		if opts.Agent || cmdutil.IsAgentError(err) {
			ae := cmdutil.ClassifyError(err)
			cmdutil.WriteAgentError(os.Stderr, ae)
			os.Exit(ae.ExitCode)
		}
		// Normal mode: plain text error.
		msg := err.Error()
		// For auth-related errors, append profile context so the user
		// knows which profile was used and how to switch.
		if isAuthRelated(msg) && opts.AuthOptions != nil {
			auth := opts.AuthOptions
			msg += fmt.Sprintf("\n  using profile %q from %s", auth.Profile, auth.CredentialsFile)
			msg += "\n  hint: run 'verda auth use' to switch profile, or 'verda auth show' to check credentials"
		}
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
}

func isAuthRelated(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "401") ||
		strings.Contains(lower, "403") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "authentication failed") ||
		strings.Contains(lower, "invalid client")
}
