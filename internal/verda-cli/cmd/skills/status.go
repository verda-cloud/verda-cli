package skills

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func NewCmdStatus(_ cmdutil.Factory, _ cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show installed skills status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}
