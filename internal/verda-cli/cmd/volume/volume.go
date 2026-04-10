package volume

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVolume creates the parent volume command.
func NewCmdVolume(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"vol"},
		Short:   "Manage volumes",
		Long: cmdutil.LongDesc(`
			List and manage Verda block storage volumes.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdDescribe(f, ioStreams),
		NewCmdAction(f, ioStreams),
		NewCmdTrash(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
