# Creating a New CLI Command

This skill documents the conventions and checklist for adding a new subcommand to verda-cli.

## Project Structure

```
internal/verda-cli/cmd/<domain>/
  <domain>.go        # Parent command (NewCmd<Domain>)
  list.go            # List subcommand
  add.go / create.go # Create/add subcommand
  delete.go          # Delete subcommand
  action.go          # Interactive action picker
```

## Checklist

Every new command MUST follow these patterns:

### 1. Command Definition (cobra)

```go
func NewCmd<Action>(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
    opts := &<action>Options{}

    cmd := &cobra.Command{
        Use:   "<action>",
        Short: "One-line description",
        Long: cmdutil.LongDesc(`
            Multi-line description.
        `),
        Example: cmdutil.Examples(`
            verda <domain> <action>
            verda <domain> <action> --flag value
        `),
        Args: cobra.NoArgs,
        RunE: func(cmd *cobra.Command, args []string) error {
            return run<Action>(cmd, f, ioStreams, opts)
        },
    }

    flags := cmd.Flags()
    // Add flags here

    return cmd
}
```

### 2. Debug Output (`--debug`)

`--debug` is a global persistent flag inherited by all subcommands. Every command that calls the API MUST include debug output.

**For mutations (create/add/delete/action)** -- log the request before execution:

```go
cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
```

**For list/read commands** -- log the API response:

```go
cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d item(s):", len(items)), items)
```

**For actions** -- log the action context:

```go
cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Action: %s on resource:", action.Label), map[string]string{
    "id":     resource.ID,
    "name":   resource.Name,
    "status": resource.Status,
})
```

### 3. Spinner Pattern

All API calls that may take time MUST show a spinner:

```go
var sp interface{ Stop(string) }
if status := f.Status(); status != nil {
    sp, _ = status.Spinner(ctx, "Loading items...")
}
result, err := client.Items.List(ctx)
if sp != nil {
    sp.Stop("")
}
if err != nil {
    return err
}
```

### 4. Timeout Context

All API calls MUST use a timeout context:

```go
ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
defer cancel()
```

### 5. Credentials Check

Commands that need API access call `f.VerdaClient()` which returns a clear error if not authenticated:

```go
client, err := f.VerdaClient()
if err != nil {
    return err
}
```

### 6. Interactive + Non-Interactive

Commands MUST support both modes:
- **Flags** for scripting/CI (non-interactive)
- **Prompter** TUI for interactive use when the target is omitted on a terminal

**Implicit trigger.** Omitting the positional target on a TTY launches the
interactive flow; everything else stays one-shot. Gate it exactly like this:

```go
if len(args) >= wantArgs {            // explicit args -> run directly
    return run(cmd, f, ioStreams, opts, args...)
}
if f.AgentMode() {                    // agents get a structured error, never a prompt
    return cmdutil.NewMissingFlagsError([]string{"s3://bucket/key"})
}
if !interactiveTTY(f) {               // non-TTY or json/yaml output -> help, no silent prompt
    return cmd.Help()
}
return runInteractive(cmd, f, ioStreams, opts, args)
```

`interactiveTTY(f)` == `IsStdoutTerminal() && !AgentMode() && OutputFormat() == "table"`.

**The hint bar is mandatory on every direct `Select` / `MultiSelect`.** Pass
`tui.WithShowHints(true)` (or `tui.WithMultiSelectShowHints(true)`) so the user
always sees the standard control legend:

```
↑/↓ navigate · type to filter · enter select · esc back · ctrl+c exit
```

(Wizard-engine step Loaders are the only exception — the composite renders its
own hint bar, so double-rendering is a bug.)

**Esc = soft back, Ctrl+C = hard exit — always. Never a confirmation dialog on
either.** Classify the prompter error; never bare-`return nil`:

```go
idx, err := f.Prompter().Select(ctx, "Pick one", labels, tui.WithShowHints(true))
if err != nil {
    if cmdutil.IsPromptCancel(err) {  // Esc OR Ctrl+C — flow doesn't care which
        return nil
    }
    return err                        // a real I/O failure MUST propagate
}
```

Use `cmdutil.IsPromptInterrupt(err)` (Ctrl+C) and `cmdutil.IsPromptBack(err)`
(Esc) when the two must differ — e.g. a "Back to list / Exit" gate, or a wizard
where Esc steps back one prompt while Ctrl+C exits the whole flow.

**Multi-step wizards — ALWAYS use the shared wizard engine.** Do NOT hand-roll a
step loop. Every multi-step interactive flow goes through
`github.com/verda-cloud/verdagostack/pkg/tui/wizard` so they all share one look
(progress bar + hint bar), Esc=back, and Ctrl+C handling. Reference flows:
`cmd/s3/wizard.go` (`buildConfigureFlow`), `cmd/s3/move_wizard.go`
(`buildMoveFlow`), `cmd/vm/wizard.go`.

Shape of a flow:

```go
engine := wizard.NewEngine(f.Prompter(), f.Status(),
    wizard.WithOutput(ioStreams.ErrOut), wizard.WithExitConfirmation())
if err := engine.Run(ctx, flow); err != nil {
    return err // Ctrl+C returns an error here — propagate it, like configure/vm/mv
}
// Final action (save / preview+confirm / execute) happens AFTER Run — the engine
// has no confirm prompt. See finalizeMove / configure's RunE.
```

```go
flow := &wizard.Flow{
    Name: "s3-move",
    Layout: []wizard.ViewDef{
        {ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
        {ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
    },
    Steps: []wizard.Step{ /* ... */ },
}
```

Per-step rules:
- Each `Step` binds to a variable via `Setter`/`Value`/`IsSet`/`Resetter`. `IsSet()==true` (e.g. a `--flag` was passed) makes the engine **skip** the step and propagate `Value()` into the collected map.
- `SelectPrompt` with a `Loader` for dynamic lists. A Loader may read earlier steps from `store.Collected()["<name>"]`; declare `DependsOn: []string{"<name>"}` so it re-runs when that input changes (e.g. mv's source-object list depends on the chosen source bucket).
- `Default(collected)` is applied **only for non-required empty values**. A `Required` step re-prompts on empty and never sees the default — so to pre-fill an optional value (e.g. dest key defaults to source key) make the step `Required: false` + `Default`.

> **Gotcha that bit us twice — conditional steps + `Resetter`:** a step skipped via
> `ShouldSkip` has its `Resetter` **called by the engine**. If that step shares a
> bound variable with another step — the "+ Create new X…" pattern, where a Select
> writes the existing choice and a conditional name step writes a new one into the
> *same* variable — its `Resetter` MUST be a **no-op**, or skipping it (an existing
> choice was picked) clobbers the selection back to the default. The owning Select
> step keeps the real `Resetter`.

> **"+ Create new…" pattern:** the Select offers a sentinel choice
> (`"\x00new-…"` — a NUL byte can't be a real bucket/profile name, so no collision).
> The Select's `Setter` must **guard against writing the sentinel** into the bound
> variable (`if s != newSentinel { x = s }`); a separate `ShouldSkip`-gated
> `TextInputPrompt` step collects the new name. See `configureStepProfile` +
> `configureStepNewProfileName`, and `moveStepDestBucket` + `moveStepNewDestBucket`.

**Testing wizards:** drive the flow with a test engine, not the tuitest prompter
mock (the engine builds its own prompt models). Use
`wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
wizard.SelectResult(i), wizard.TextResult(s), …))` and assert the bound state
after `engine.Run`. Test the post-`Run` action (save/confirm/execute) separately
with the tuitest prompter. See `TestBuildConfigureFlowHappyPath`,
`TestBuildMoveFlow_CollectsSelections`, `TestFinalizeMove_S3ToS3`.

### 7. Output Conventions

- Normal output goes to `ioStreams.Out`
- Prompts, warnings, and debug go to `ioStreams.ErrOut`
- Use lipgloss styles from `charm.land/lipgloss/v2`:
  - `lipgloss.Color("8")` -- dim/gray
  - `lipgloss.Color("1")` -- red/error
  - `lipgloss.Color("2")` -- green/price
  - `lipgloss.Color("3")` -- yellow/warning
  - `lipgloss.Color("14")` -- cyan/accent
- Bold: `lipgloss.NewStyle().Bold(true)`

### 8. Confirmation for Destructive Actions

Delete and dangerous actions MUST confirm:

```go
confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Delete %s?", name))
if err != nil {
    if cmdutil.IsPromptCancel(err) {        // Esc/Ctrl+C = clean cancel
        _, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
        return nil
    }
    return err                              // real I/O failure must propagate
}
if !confirmed {
    _, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
    return nil
}
```

(Note American spelling: `Canceled`. In `--agent` mode require `--yes` and never
prompt; without it, return `cmdutil.NewConfirmationRequiredError(<verb>)`.)

For dangerous actions, add warning styling:

```go
warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", warnStyle.Render("This action cannot be undone."))
```

### 9. Interactive Select with Cancel

Always append a "Cancel" option and handle Esc:

```go
labels = append(labels, "Cancel")
idx, err := prompter.Select(ctx, "Select item", labels)
if err != nil {
    return nil // Esc/Ctrl+C
}
if idx == len(items) { // Cancel
    return nil
}
```

### 10. Register in Parent Command

Add the new command to its parent in `<domain>.go`:

```go
cmd.AddCommand(
    NewCmd<Action>(f, ioStreams),
)
```

### 11. Register Domain in Root

If creating a new domain, add to `cmd/cmd.go` in the appropriate command group.

### 12. Long Lists

For commands that return potentially long lists, use the pager:

```go
if status := f.Status(); status != nil {
    return status.Pager(cmd.Context(), content, tui.WithPagerTitle("Title"))
}
_, _ = fmt.Fprint(ioStreams.Out, content)
```

The pager auto-detects: prints directly if content fits terminal, otherwise shows scrollable viewport.

## Dependencies

- `cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"` -- Factory, IOStreams, DebugJSON, helpers
- `"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"` -- SDK client and types
- `"github.com/verda-cloud/verdagostack/pkg/tui"` -- Prompter, Status, pager options
- `"charm.land/lipgloss/v2"` -- Terminal styling
- `"github.com/spf13/cobra"` -- Command framework

## Wizard Flows

For complex multi-step creation flows, use the wizard engine:

```go
flow := &wizard.Flow{
    Name: "resource-create",
    Layout: []wizard.ViewDef{
        {ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
    },
    Steps: []wizard.Step{ ... },
}
engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
return engine.Run(ctx, flow)
```

Key wizard patterns:
- Steps that handle their own prompting inside Loader should have no-op Setter/Resetter
- Use `clientFunc` for lazy API client resolution
- Use `apiCache` to share data between steps
- Use `withSpinner` helper for API calls inside step Loaders

## Hostname Generation

Use `cmdutil.GenerateHostname(locationCode)` for auto-generated hostnames (3 random words + location).
Use `cmdutil.ValidateHostname(s)` for validation (letters, digits, hyphens, no leading/trailing hyphen).
