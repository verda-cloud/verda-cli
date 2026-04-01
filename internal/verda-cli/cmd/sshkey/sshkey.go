package sshkey

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSSHKey creates the parent ssh-key command.
func NewCmdSSHKey(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ssh-key",
		Short:   "Manage SSH keys",
		Long: cmdutil.LongDesc(`
			Create, list, and delete SSH keys used for VM authentication.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdList(f, ioStreams),
		NewCmdAdd(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
