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

package settings

import (
	"fmt"
	"slices"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewCmdTheme creates the settings theme command.
func NewCmdTheme(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme [name]",
		Short: "View or change the CLI color theme",
		Long: cmdutil.LongDesc(`
			View and change the CLI color theme.

			Without arguments, opens an interactive theme selector.
			With a theme name, sets it directly.
		`),
		Example: cmdutil.Examples(`
			# Interactive selection
			verda settings theme

			# Set a theme directly
			verda settings theme dracula
		`),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeThemeNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return setTheme(ioStreams, args[0])
			}
			return selectThemeWizard(cmd, f, ioStreams)
		},
	}

	return cmd
}

func selectThemeWizard(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	current := bubbletea.GetThemeName()
	var selected string

	names := bubbletea.ThemeNames()
	slices.Sort(names)
	choices := make([]wizard.Choice, len(names))
	for i, name := range names {
		t := bubbletea.Themes[name]
		label := renderThemePreview(name, &t)
		if name == current {
			label += "  (current)"
		}
		choices[i] = wizard.Choice{Label: label, Value: name}
	}

	flow := &wizard.Flow{
		Name: "theme-select",
		Layout: []wizard.ViewDef{
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			{
				Name:        "theme",
				Description: "Select theme",
				Prompt:      wizard.SelectPrompt,
				Loader:      wizard.StaticChoices(choices...),
				Default:     func(_ map[string]any) any { return current },
				Setter:      func(v any) { selected = v.(string) },
				IsSet:       func() bool { return selected != "" },
				Value:       func() any { return selected },
			},
		},
	}

	engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
	if err := engine.Run(cmd.Context(), flow); err != nil {
		return nil //nolint:nilerr // User pressed Esc/Ctrl+C.
	}

	if selected == "" || selected == current {
		return nil
	}

	return setTheme(ioStreams, selected)
}

func setTheme(ioStreams cmdutil.IOStreams, name string) error {
	if !bubbletea.SetThemeByName(name) {
		names := bubbletea.ThemeNames()
		slices.Sort(names)
		return fmt.Errorf("unknown theme %q\n\nAvailable themes: %v", name, names)
	}

	if err := options.SaveSetting("settings.theme", name); err != nil {
		return fmt.Errorf("saving theme: %w", err)
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Theme set to %s\n", name)
	return nil
}

func renderThemePreview(name string, theme *bubbletea.Theme) string {
	if theme.NoColor {
		bold := lipgloss.NewStyle().Bold(true)
		faint := lipgloss.NewStyle().Faint(true)
		rev := lipgloss.NewStyle().Reverse(true)
		uline := lipgloss.NewStyle().Bold(true).Underline(true)
		return fmt.Sprintf("%-16s  %s %s %s %s",
			name,
			bold.Render("bold"),
			faint.Render("faint"),
			rev.Render("reverse"),
			uline.Render("error"))
	}

	accent := lipgloss.NewStyle().Foreground(theme.Accent)
	success := lipgloss.NewStyle().Foreground(theme.Success)
	errStyle := lipgloss.NewStyle().Foreground(theme.Error)
	dim := lipgloss.NewStyle().Foreground(theme.Dim)

	return fmt.Sprintf("%-16s  %s %s %s %s",
		name,
		accent.Render("accent"),
		success.Render("success"),
		errStyle.Render("error"),
		dim.Render("dim"))
}

func completeThemeNames(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names := bubbletea.ThemeNames()
	return names, cobra.ShellCompDirectiveNoFileComp
}
