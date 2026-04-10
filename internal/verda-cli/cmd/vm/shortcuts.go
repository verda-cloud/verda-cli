package vm

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
		Action: verda.ActionStart,
	},
	{
		Use:    "shutdown <instance-id>",
		Short:  "Shutdown a VM instance",
		Action: verda.ActionShutdown,
	},
	{
		Use:    "hibernate <instance-id>",
		Short:  "Hibernate a VM instance",
		Action: verda.ActionHibernate,
	},
	{
		Use:     "delete <instance-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a VM instance",
		Action:  verda.ActionDelete,
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

	cmd.Example = shortcutExamples(def)

	cmd.Flags().StringVar(&opts.InstanceID, "id", "", "Instance ID (alternative to positional argument)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Target all instances (use with --status/--hostname to filter)")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter by status, requires --all (e.g., running, offline)")
	cmd.Flags().StringVar(&opts.Hostname, "hostname", "", "Filter by hostname glob pattern, requires --all (e.g., \"test-*\")")
	cmd.Flags().BoolVar(&opts.WithVolumes, "with-volumes", false, "Also delete all attached volumes (delete only)")
	opts.Wait.AddFlags(cmd.Flags(), true)

	if def.Action != verda.ActionDelete {
		_ = cmd.Flags().MarkHidden("with-volumes")
	}

	return cmd
}

func shortcutExamples(def shortcutDef) string {
	name := strings.SplitN(def.Use, " ", 2)[0]

	examples := fmt.Sprintf(`# %s a specific instance
verda vm %s inst-abc-123

# Interactive: select from list
verda vm %s

# Batch: %s all matching instances
verda vm %s --all --status %s

# Batch: %s instances matching hostname pattern
verda vm %s --all --hostname "test-*"

# Batch: combine filters (AND logic)
verda vm %s --all --status %s --hostname "test-*"`, def.Short, name, name, strings.ToLower(strings.SplitN(def.Short, " ", 2)[0]), name, exampleStatus(def.Action), strings.ToLower(strings.SplitN(def.Short, " ", 2)[0]), name, name, exampleStatus(def.Action))

	if def.Action == verda.ActionDelete {
		examples += fmt.Sprintf(`

# Batch delete with volumes
verda vm %s --all --status offline --with-volumes --yes`, name)
	}

	return cmdutil.Examples(examples)
}

func exampleStatus(action string) string {
	switch action {
	case verda.ActionStart:
		return "offline"
	case verda.ActionDelete:
		return "offline"
	default:
		return "running"
	}
}
