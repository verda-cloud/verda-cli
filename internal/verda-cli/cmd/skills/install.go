package skills

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func NewCmdInstall(_ cmdutil.Factory, _ cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "install [agents...]",
		Short: "Install AI agent skills for Verda Cloud",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
}
