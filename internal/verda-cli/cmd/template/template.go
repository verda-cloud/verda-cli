package template

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdTemplate creates the parent template command.
func NewCmdTemplate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"tmpl"},
		Short:   "Manage reusable resource templates",
		Long: cmdutil.LongDesc(`
			Save, list, show, and delete reusable resource configuration templates.
			Templates pre-fill the create wizard so you don't repeat the same settings.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdShow(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
