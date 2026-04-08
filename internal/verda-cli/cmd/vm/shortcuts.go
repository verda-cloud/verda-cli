package vm

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// shortcutDef describes a shortcut command that delegates to runAction.
type shortcutDef struct {
	Use     string
	Aliases []string
	Short   string
	Action  string // value passed as --action to runAction
}

var shortcuts = []shortcutDef{
	{
		Use:    "start <instance-id>",
		Short:  "Start a VM instance",
		Action: "start",
	},
	{
		Use:     "shutdown <instance-id>",
		Short:   "Shutdown a VM instance",
		Aliases: []string{"stop"},
		Action:  "shutdown",
	},
	{
		Use:    "hibernate <instance-id>",
		Short:  "Hibernate a VM instance",
		Action: "hibernate",
	},
	{
		Use:     "delete <instance-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a VM instance",
		Action:  "delete",
	},
}

// newShortcutCmd creates a thin command that pre-sets the action and delegates to runAction.
func newShortcutCmd(f cmdutil.Factory, ioStreams cmdutil.IOStreams, def shortcutDef) *cobra.Command {
	opts := &actionOptions{
		Action: def.Action,
	}

	cmd := &cobra.Command{
		Use:     def.Use,
		Aliases: def.Aliases,
		Short:   def.Short,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.InstanceID = args[0]
			}
			return runAction(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.InstanceID, "id", "", "Instance ID (alternative to positional argument)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Target all instances matching filters")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter instances by status (e.g., running, offline)")
	cmd.Flags().BoolVar(&opts.WithVolumes, "with-volumes", false, "Also delete all attached volumes (delete only)")
	opts.Wait.AddFlags(cmd.Flags(), true)

	if def.Action != "delete" {
		_ = cmd.Flags().MarkHidden("with-volumes")
	}

	return cmd
}
