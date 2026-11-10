// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// TagAnnotation marks a command with a short status label (e.g. "beta") shown
// next to its name in the grouped root help. Set via cmd.Annotations.
const TagAnnotation = "verda.tag"

// TagSuffix returns " (<tag>)" for a command with TagAnnotation, else "".
// The root help and the generated docs both use it, so they cannot drift.
func TagSuffix(c *cobra.Command) string {
	if tag := c.Annotations[TagAnnotation]; tag != "" {
		return " (" + tag + ")"
	}
	return ""
}

// CommandGroup represents a logical group of subcommands with a heading message.
type CommandGroup struct {
	Message  string
	Commands []*cobra.Command
}

// CommandGroups is an ordered list of command groups.
type CommandGroups []CommandGroup

// orderAnnotation: declaration rank set by Add. Cobra sorts Commands() in
// place, so it is the only record of the order the grouped --help shows.
const orderAnnotation = "verda.order"

// Add registers every command in each group as a subcommand of c, mirroring
// the groups as native cobra groups so docgen can read their order and titles.
func (g CommandGroups) Add(c *cobra.Command) {
	rank := 0
	for _, group := range g {
		c.AddGroup(&cobra.Group{ID: group.Message, Title: group.Message})
		for _, sub := range group.Commands {
			sub.GroupID = group.Message
			if sub.Annotations == nil {
				sub.Annotations = map[string]string{}
			}
			sub.Annotations[orderAnnotation] = strconv.Itoa(rank)
			rank++
		}
		c.AddCommand(group.Commands...)
	}
}

// DeclarationOrder returns c's position in the CommandGroups that added it;
// ok is false for commands not added through CommandGroups.Add.
func DeclarationOrder(c *cobra.Command) (rank int, ok bool) {
	rank, err := strconv.Atoi(c.Annotations[orderAnnotation])
	return rank, err == nil
}

// Has returns true if c belongs to any group.
func (g CommandGroups) Has(c *cobra.Command) bool {
	for _, group := range g {
		if slices.Contains(group.Commands, c) {
			return true
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
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableSubCommands}}

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
			if c.Hidden {
				continue
			}
			fmt.Fprintf(&b, "  %-18s %s\n", c.Name()+TagSuffix(c), c.Short)
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
