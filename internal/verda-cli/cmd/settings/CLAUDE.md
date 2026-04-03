# Settings Command Knowledge

## Quick Reference
- Parent: `verda settings`
- Subcommands: `theme [name]`
- Files:
  - `settings.go` -- Parent command; registers `theme` subcommand
  - `theme.go` -- Theme selection (interactive wizard or direct set), preview rendering, shell completion

## Domain-Specific Logic
- Theme names and definitions come from `bubbletea.Themes` map and `bubbletea.ThemeNames()`
- In-memory theme is applied via `bubbletea.SetThemeByName(name)` which returns `false` for unknown themes
- Persistence uses `options.SaveSetting("settings.theme", name)` to write to `~/.verda/config.yaml`
- Theme preview rendering has two code paths: `NoColor` themes use text styling (bold, faint, reverse, underline) while color themes show accent/success/error/dim color swatches
- Shell completions are registered via `ValidArgsFunction` returning all theme names with `ShellCompDirectiveNoFileComp`

## Gotchas & Edge Cases
- When the user cancels the interactive wizard (Esc/Ctrl+C), `selectThemeWizard` returns `nil` (not the wizard error). This is intentional -- see the `//nolint:nilerr` comment.
- If the selected theme equals the current theme, no write occurs (early return).
- If the selected theme is empty (wizard produced no selection), no write occurs.
- Unknown theme names produce an error listing all available themes.
- Theme choices are sorted alphabetically before display in the wizard.

## Relationships
- `cmdutil.Factory` / `cmdutil.IOStreams` -- standard dependency injection
- `options` package -- `SaveSetting()` for persisting to config YAML
- `verdagostack/pkg/tui/wizard` -- wizard engine, `SelectPrompt`, `StaticChoices`, `NewHintBarView`
- `verdagostack/pkg/tui/bubbletea` -- `Themes`, `ThemeNames()`, `GetThemeName()`, `SetThemeByName()`, `HintStyle()`, `Theme` type
- `charm.land/lipgloss/v2` -- used in `renderThemePreview` for styled color swatches
