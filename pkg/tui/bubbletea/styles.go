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

package bubbletea

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme defines the color palette for all TUI prompts.
type Theme struct {
	Accent  color.Color // cursor, selected items, final answers
	Success color.Color // prompt "?", checkmarks
	Error   color.Color // validation errors
	Dim     color.Color // unselected items
	Hint    color.Color // keyboard hints (brighter than Dim)
	NoColor bool        // when true, use bold/faint/reverse instead of ANSI colors
}

// Built-in themes.
var (
	// ThemeDefault uses standard terminal ANSI colors.
	ThemeDefault = Theme{
		Accent:  lipgloss.Color("14"), // cyan
		Success: lipgloss.Color("10"), // green
		Error:   lipgloss.Color("9"),  // red
		Dim:     lipgloss.Color("8"),  // gray
		Hint:    lipgloss.Color("7"),  // light gray
	}

	// ThemeDracula uses the Dracula color palette.
	ThemeDracula = Theme{
		Accent:  lipgloss.Color("#bd93f9"), // purple
		Success: lipgloss.Color("#50fa7b"), // green
		Error:   lipgloss.Color("#ff5555"), // red
		Dim:     lipgloss.Color("#6272a4"), // comment
		Hint:    lipgloss.Color("#8592b8"), // brighter comment
	}

	// ThemeCatppuccin uses the Catppuccin Mocha palette.
	ThemeCatppuccin = Theme{
		Accent:  lipgloss.Color("#89b4fa"), // blue
		Success: lipgloss.Color("#a6e3a1"), // green
		Error:   lipgloss.Color("#f38ba8"), // red
		Dim:     lipgloss.Color("#6c7086"), // overlay0
		Hint:    lipgloss.Color("#9399b2"), // overlay2
	}

	// ThemeNord uses the Nord color palette.
	ThemeNord = Theme{
		Accent:  lipgloss.Color("#88c0d0"), // frost
		Success: lipgloss.Color("#a3be8c"), // green
		Error:   lipgloss.Color("#bf616a"), // red
		Dim:     lipgloss.Color("#4c566a"), // polar night
		Hint:    lipgloss.Color("#616e88"), // brighter polar night
	}

	// ThemeTokyoNight uses the Tokyo Night palette.
	ThemeTokyoNight = Theme{
		Accent:  lipgloss.Color("#7aa2f7"), // blue
		Success: lipgloss.Color("#9ece6a"), // green
		Error:   lipgloss.Color("#f7768e"), // red
		Dim:     lipgloss.Color("#565f89"), // comment
		Hint:    lipgloss.Color("#737aa2"), // brighter comment
	}

	// ThemeGitHubLight is a light theme based on the GitHub Light palette.
	ThemeGitHubLight = Theme{
		Accent:  lipgloss.Color("#0969da"), // blue
		Success: lipgloss.Color("#1a7f37"), // green
		Error:   lipgloss.Color("#cf222e"), // red
		Dim:     lipgloss.Color("#656d76"), // gray
		Hint:    lipgloss.Color("#57606a"), // darker gray
	}

	// ThemeCatppuccinLatte is the light variant of the Catppuccin palette.
	ThemeCatppuccinLatte = Theme{
		Accent:  lipgloss.Color("#1e66f5"), // blue
		Success: lipgloss.Color("#40a02b"), // green
		Error:   lipgloss.Color("#d20f39"), // red
		Dim:     lipgloss.Color("#9ca0b0"), // overlay0
		Hint:    lipgloss.Color("#7c7f93"), // overlay1
	}

	// ThemeSolarizedLight uses the Solarized Light palette.
	ThemeSolarizedLight = Theme{
		Accent:  lipgloss.Color("#268bd2"), // blue
		Success: lipgloss.Color("#859900"), // green
		Error:   lipgloss.Color("#dc322f"), // red
		Dim:     lipgloss.Color("#93a1a1"), // base1
		Hint:    lipgloss.Color("#657b83"), // base00
	}

	// ThemeMonoDark is a color-free theme for dark-background terminals.
	// Uses only bold, faint, and reverse attributes — no ANSI colors.
	ThemeMonoDark = Theme{NoColor: true}

	// ThemeMonoLight is a color-free theme for light-background terminals.
	// Uses only bold, faint, and reverse attributes — no ANSI colors.
	ThemeMonoLight = Theme{NoColor: true}
)

// activeTheme is the current theme. Defaults to ThemeDefault.
var activeTheme = ThemeDefault

// activeThemeName tracks which theme is active by name.
var activeThemeName = "default"

// Themes maps theme names to Theme values.
// Use this for CLI flag validation or listing available themes.
var Themes = map[string]Theme{
	"default":          ThemeDefault,
	"dracula":          ThemeDracula,
	"catppuccin":       ThemeCatppuccin,
	"nord":             ThemeNord,
	"tokyonight":       ThemeTokyoNight,
	"github-light":     ThemeGitHubLight,
	"catppuccin-latte": ThemeCatppuccinLatte,
	"solarized-light":  ThemeSolarizedLight,
	"mono-dark":        ThemeMonoDark,
	"mono-light":       ThemeMonoLight,
}

// SetTheme changes the color theme for all TUI prompts.
// Call this before creating any prompts (typically in main or init).
func SetTheme(t Theme) {
	activeTheme = t
	// Try to resolve the name.
	activeThemeName = "custom"
	for name, theme := range Themes {
		if theme == t {
			activeThemeName = name
			break
		}
	}
	applyTheme()
}

// SetThemeByName sets the theme by name. Returns false if the name is unknown.
func SetThemeByName(name string) bool {
	t, ok := Themes[name]
	if !ok {
		return false
	}
	activeTheme = t
	activeThemeName = name
	applyTheme()
	return true
}

// GetTheme returns the active theme.
func GetTheme() Theme {
	return activeTheme
}

// HintStyle returns the resolved hint lipgloss.Style for the active theme.
// Use this with wizard.WithHintStyle to pass the correct style to HintBarView.
func HintStyle() lipgloss.Style {
	return hintStyle
}

// GetThemeName returns the name of the active theme
// (e.g., "default", "dracula", "catppuccin", "nord", "tokyonight", or "custom").
func GetThemeName() string {
	return activeThemeName
}

// ThemeNames returns the names of all built-in themes.
func ThemeNames() []string {
	return []string{"default", "dracula", "catppuccin", "catppuccin-latte", "nord", "tokyonight", "github-light", "solarized-light", "mono-dark", "mono-light"}
}

// Shared styles derived from the active theme.
var (
	promptStyle   lipgloss.Style
	titleStyle    lipgloss.Style
	answerStyle   lipgloss.Style
	cursorStyle   lipgloss.Style
	selectedStyle lipgloss.Style
	dimStyle      lipgloss.Style
	checkStyle    lipgloss.Style
	uncheckStyle  lipgloss.Style
	errorStyle    lipgloss.Style
	hintStyle     lipgloss.Style
)

func init() {
	applyTheme()
}

func applyTheme() {
	t := activeTheme

	if t.NoColor {
		applyNoColorTheme()
		return
	}

	promptStyle = lipgloss.NewStyle().Foreground(t.Success).Bold(true)
	titleStyle = lipgloss.NewStyle().Bold(true)
	answerStyle = lipgloss.NewStyle().Foreground(t.Accent)
	cursorStyle = lipgloss.NewStyle().Foreground(t.Accent)
	selectedStyle = lipgloss.NewStyle().Foreground(t.Accent)
	dimStyle = lipgloss.NewStyle().Foreground(t.Dim)
	checkStyle = lipgloss.NewStyle().Foreground(t.Success)
	uncheckStyle = lipgloss.NewStyle().Foreground(t.Dim)
	errorStyle = lipgloss.NewStyle().Foreground(t.Error)
	hintStyle = lipgloss.NewStyle().Foreground(t.Hint).Bold(true)
}

// applyNoColorTheme sets styles using only bold, faint, reverse, and
// underline — no ANSI color codes. Works on monochrome terminals and
// when TERM=dumb or NO_COLOR is set.
func applyNoColorTheme() {
	reverse := activeThemeName == "mono-light"

	promptStyle = lipgloss.NewStyle().Bold(true)
	titleStyle = lipgloss.NewStyle().Bold(true)
	dimStyle = lipgloss.NewStyle().Faint(true)
	hintStyle = lipgloss.NewStyle().Faint(true)
	uncheckStyle = lipgloss.NewStyle().Faint(true)
	errorStyle = lipgloss.NewStyle().Bold(true).Underline(true)

	if reverse {
		// Light background: reverse for accent, bold for answers.
		answerStyle = lipgloss.NewStyle().Reverse(true)
		cursorStyle = lipgloss.NewStyle().Reverse(true)
		selectedStyle = lipgloss.NewStyle().Reverse(true)
		checkStyle = lipgloss.NewStyle().Reverse(true)
	} else {
		// Dark background: bold for accent, underline for answers.
		answerStyle = lipgloss.NewStyle().Bold(true)
		cursorStyle = lipgloss.NewStyle().Bold(true)
		selectedStyle = lipgloss.NewStyle().Bold(true)
		checkStyle = lipgloss.NewStyle().Bold(true)
	}
}
