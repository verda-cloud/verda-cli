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

package skills

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func NewCmdSkills(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage AI agent skills for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Install, update, and manage AI agent skill files that teach coding
			agents how to use the Verda CLI for cloud infrastructure management.

			Skills teach AI agents to:
			  - Discover instance types, availability, and pricing
			  - Deploy GPU/CPU VMs with proper dependency chains
			  - Manage lifecycle (start, stop, delete) with safety checks
			  - Handle costs, volumes, SSH keys, and startup scripts
			  - Use --agent and -o json flags correctly

			Skills are bundled with the CLI binary — no network fetch needed.
			They are versioned with the CLI, so updating the CLI updates skills.

			Supported agents: Claude Code, Cursor, Windsurf, Codex, Gemini CLI, Copilot.
			Custom agents can be added via ~/.verda/agents.json.
		`),
		Example: cmdutil.Examples(`
			# Install skills for your AI coding agent
			verda skills install claude-code

			# Install for multiple agents at once
			verda skills install claude-code cursor windsurf

			# Check installed version and available updates
			verda skills status

			# Add a custom agent via ~/.verda/agents.json:
			# { "agents": { "aider": { "display_name": "Aider",
			#   "scope": "project", "target": ".aider/instructions.md",
			#   "method": "append" } } }
			verda skills install aider
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdInstall(f, ioStreams),
		NewCmdStatus(f, ioStreams),
		NewCmdUninstall(f, ioStreams),
	)

	return cmd
}
