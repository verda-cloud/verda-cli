package skills

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func NewCmdSkills(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage AI agent skills for Verda Cloud",
		Long: cmdutil.LongDesc(`
			Install, update, and manage AI agent skill files that teach coding
			agents (Claude Code, Cursor, Windsurf, Codex, Gemini CLI, Copilot)
			how to use the Verda CLI for cloud infrastructure management.

			Skills are bundled with the CLI. User-defined agents can be added
			via ~/.verda/agents.json.
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
