package skills

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func NewCmdUninstall(_ cmdutil.Factory, _ cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall [agents...]",
		Short: "Remove installed AI agent skills",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
}
