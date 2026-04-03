# verda settings -- Manage CLI settings

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda settings theme [name]` | View or change the CLI color theme | (positional arg: theme name) |

## Usage Examples

### Theme
```bash
# Interactive theme selector
verda settings theme

# Set a theme directly
verda settings theme dracula
```

## Interactive vs Non-Interactive

The `theme` command runs an interactive wizard (select prompt) when called without arguments. When a theme name is provided as a positional argument, it sets the theme directly without any prompts.

## Architecture Notes

- **settings.go** -- Parent command registration; adds the `theme` subcommand. Settings are stored in `~/.verda/config.yaml`.
- **theme.go** -- Theme viewing and switching. Contains:
  - `selectThemeWizard` -- Interactive selection using wizard engine with a single `SelectPrompt` step. Lists all available themes sorted alphabetically, marks the current one with "(current)". Returns `nil` (not an error) if the user cancels.
  - `setTheme` -- Validates the theme name against `bubbletea.Themes`, calls `bubbletea.SetThemeByName()` to apply it in-memory, and persists to config via `options.SaveSetting("settings.theme", name)`.
  - `renderThemePreview` -- Generates a color-swatch preview string for each theme in the selection list. Handles `NoColor` themes differently (uses bold/faint/reverse/underline styles instead of colors).
  - `completeThemeNames` -- Shell completion function for theme names.

### Theme Storage

- Persisted at YAML path `settings.theme` in `~/.verda/config.yaml` via `options.SaveSetting`
- Available themes come from `bubbletea.Themes` map; names from `bubbletea.ThemeNames()`
