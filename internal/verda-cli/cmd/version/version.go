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
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Get()
			if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), info); wrote {
				return err
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "  Version:   %s\n  Platform:  %s\n", info.GitVersion, info.Platform)
			return nil
		},
	}
}
