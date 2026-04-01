package volume

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVolume creates the parent volume command.
func NewCmdVolume(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"vol"},
		Short:   "Manage volumes",
		Long: cmdutil.LongDesc(`
			List and delete Verda block storage volumes.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdList(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
