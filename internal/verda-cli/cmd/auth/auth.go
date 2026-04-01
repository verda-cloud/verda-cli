package auth

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdAuth creates the parent auth command.
func NewCmdAuth(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage shared credentials and profiles",
		Long: cmdutil.LongDesc(`
			Manage Verda shared credentials and the active auth profile.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdConfigure(f, ioStreams),
		NewCmdUse(f, ioStreams),
		NewCmdShow(f, ioStreams),
	)

	return cmd
}
