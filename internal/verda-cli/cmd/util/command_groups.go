package util

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// CommandGroup represents a logical group of subcommands with a heading message.
type CommandGroup struct {
	Message  string
	Commands []*cobra.Command
}

// CommandGroups is an ordered list of command groups.
type CommandGroups []CommandGroup

// Add registers every command in each group as a subcommand of c.
func (g CommandGroups) Add(c *cobra.Command) {
	for _, group := range g {
		c.AddCommand(group.Commands...)
	}
}

// Has returns true if c belongs to any group.
func (g CommandGroups) Has(c *cobra.Command) bool {
	for _, group := range g {
		for _, cmd := range group.Commands {
			if cmd == c {
				return true
			}
		}
	}
	return false
}

const defaultUsageTemplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

// SetUsageTemplate configures a grouped usage template on the root command
// and resets all child commands to the default cobra template.
func SetUsageTemplate(cmd *cobra.Command, groups CommandGroups) {
	var b strings.Builder
	b.WriteString("Usage:\n  {{.CommandPath}} [command]\n\n")

	for _, group := range groups {
		b.WriteString(group.Message)
		b.WriteString("\n")
		for _, c := range group.Commands {
			b.WriteString(fmt.Sprintf("  %-18s %s\n", c.Name(), c.Short))
		}
		b.WriteString("\n")
	}

	b.WriteString("Flags:\n{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}\n\n")
	b.WriteString("Use \"{{.CommandPath}} [command] --help\" for more information about a command.\n")

	cmd.SetUsageTemplate(b.String())
	resetChildTemplates(cmd)
}

func resetChildTemplates(parent *cobra.Command) {
	for _, c := range parent.Commands() {
		c.SetUsageTemplate(defaultUsageTemplate)
		resetChildTemplates(c)
	}
}
