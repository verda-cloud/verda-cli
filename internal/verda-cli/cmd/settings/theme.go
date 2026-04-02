package settings

import (
	"fmt"
	"slices"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// NewCmdTheme creates the settings theme command.
func NewCmdTheme(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme [name]",
		Short: "View or change the CLI color theme",
		Long: cmdutil.LongDesc(`
			View the current theme, list available themes, or set a new theme.

			Without arguments, shows the current theme and lists available options.
			With a theme name, sets it as the active theme.
		`),
		Example: cmdutil.Examples(`
			# Show current theme and list available
			verda settings theme

			# Set a theme
			verda settings theme dracula

			# Interactive selection
			verda settings theme --select
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return setTheme(f, ioStreams, args[0])
			}
			selectFlag, _ := cmd.Flags().GetBool("select")
			if selectFlag {
				return selectTheme(cmd, f, ioStreams)
			}
			return showTheme(ioStreams)
		},
	}

	cmd.Flags().BoolP("select", "s", false, "Interactively select a theme")

	return cmd
}

func showTheme(ioStreams cmdutil.IOStreams) error {
	current := bubbletea.GetThemeName()
	names := bubbletea.ThemeNames()
	slices.Sort(names)

	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s  %s\n\n", bold.Render("Current theme:"), current)

	for _, name := range names {
		theme := bubbletea.Themes[name]
		marker := "  "
		if name == current {
			marker = "* "
		}
		preview := renderThemePreview(name, theme)
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s%s\n", marker, preview)
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s\n\n", dim.Render("Use \"verda settings theme <name>\" or \"verda settings theme --select\""))
	return nil
}

func selectTheme(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	current := bubbletea.GetThemeName()
	names := bubbletea.ThemeNames()
	slices.Sort(names)

	labels := make([]string, len(names))
	for i, name := range names {
		theme := bubbletea.Themes[name]
		label := renderThemePreview(name, theme)
		if name == current {
			label += "  (current)"
		}
		labels[i] = label
	}

	prompter := f.Prompter()
	idx, err := prompter.Select(cmd.Context(), "Select theme", labels)
	if err != nil {
		return nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
	}

	return setTheme(f, ioStreams, names[idx])
}

func setTheme(f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string) error {
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

func renderThemePreview(name string, theme bubbletea.Theme) string {
	accent := lipgloss.NewStyle().Foreground(theme.Accent)
	success := lipgloss.NewStyle().Foreground(theme.Success)
	errStyle := lipgloss.NewStyle().Foreground(theme.Error)
	dim := lipgloss.NewStyle().Foreground(theme.Dim)

	return fmt.Sprintf("%-12s  %s %s %s %s",
		name,
		accent.Render("accent"),
		success.Render("success"),
		errStyle.Render("error"),
		dim.Render("dim"))
}
