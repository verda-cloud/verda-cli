package version

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/verda-cloud/verdagostack/pkg/version"
	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVersion creates the version cobra command.
func NewCmdVersion(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version information",
		Long:  cmdutil.LongDesc("Print the build and version information for verda."),
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(ioStreams.Out, version.Get().ToJSON())
		},
	}
}
