package vm

import (
	"fmt"
	"strings"

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
		Use:    "stop <instance-id>",
		Short:  "Stop a VM instance",
		Action: "shutdown",
	},
	{
		Use:    "shutdown <instance-id>",
		Short:  "Shutdown a VM instance",
		Action: "shutdown",
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

	cmd.Example = shortcutExamples(def)

	cmd.Flags().StringVar(&opts.InstanceID, "id", "", "Instance ID (alternative to positional argument)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Target all instances matching filters")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter instances by status (e.g., running, offline)")
	cmd.Flags().StringVar(&opts.Hostname, "hostname", "", "Filter instances by hostname glob pattern (e.g., \"test-*\")")
	cmd.Flags().BoolVar(&opts.WithVolumes, "with-volumes", false, "Also delete all attached volumes (delete only)")
	opts.Wait.AddFlags(cmd.Flags(), true)

	if def.Action != "delete" {
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
verda vm %s --hostname "test-*"`, def.Short, name, name, strings.ToLower(strings.SplitN(def.Short, " ", 2)[0]), name, exampleStatus(def.Action), strings.ToLower(strings.SplitN(def.Short, " ", 2)[0]), name)

	if def.Action == "delete" {
		examples += fmt.Sprintf(`

# Batch delete with volumes
verda vm %s --all --status offline --with-volumes --yes`, name)
	}

	return cmdutil.Examples(examples)
}

func exampleStatus(action string) string {
	switch action {
	case "start":
		return "offline"
	case "delete":
		return "offline"
	default:
		return "running"
	}
}
