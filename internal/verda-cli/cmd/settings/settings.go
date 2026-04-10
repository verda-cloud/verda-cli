package settings

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSettings creates the parent settings command.
func NewCmdSettings(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage CLI settings",
		Long: cmdutil.LongDesc(`
			View and update CLI settings stored in ~/.verda/config.yaml.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdTheme(f, ioStreams),
	)

	return cmd
}
