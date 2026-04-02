# HintBarView + Wizard Refactor

**Goal:** Add a contextual hint bar to all interactive commands, and unify action commands under the wizard engine for consistent UX.

**Architecture:** Build a `HintBarView` in verdagostack's wizard package that auto-renders hints based on the current step's PromptType. Convert action/create/delete commands to wizard flows. Keep list commands standalone with component-level hints (Approach B).

## Part 1: verdagostack — HintBarView

### Task 1: Create HintBarView

New file: `pkg/tui/wizard/view_hintbar.go`

- Implements `View` interface
- Subscribes to `StepChangedMsg`
- Renders hint based on PromptType:

| PromptType | Hint |
|-----------|------|
| SelectPrompt | `↑/↓ navigate · type to filter · enter select · esc back` |
| MultiSelectPrompt | `↑/↓ navigate · space toggle · enter confirm · esc back` |
| TextInputPrompt | `enter submit · esc cancel` |
| ConfirmPrompt | `y/n · enter confirm` |
| PasswordPrompt | `enter submit · esc cancel` |

- `StepChangedMsg` needs to include `PromptType` (currently has Current, Total, StepName, Collected)
- Constructor: `NewHintBarView() *HintBarView`
- Render as dim styled single line

### Task 2: Add PromptType to StepChangedMsg

Update `pkg/tui/wizard/view.go`:

```go
type StepChangedMsg struct {
    Current    int
    Total      int
    StepName   string
    PromptType PromptType  // NEW
    Collected  map[string]any
}
```

Update engine.go broadcast to include PromptType.

### Task 3: Component-level hints for standalone prompts (Approach B)

Update Select and MultiSelect View() to append a dim hint line below the list:

- Select: `↑/↓ navigate · type to filter · enter select · esc cancel`
- MultiSelect: `↑/↓ navigate · space toggle · enter confirm · esc cancel`

This covers list commands and any prompt used outside the wizard.

Remove the inline `(type to filter, enter to select)` / `(space to toggle, enter to confirm)` from the prompt title line — the footer replaces it.

## Part 2: verda-cli — Convert commands to wizard flows

### Commands to convert (wizard):

1. **ssh-key add** — Steps: name → public-key
2. **ssh-key delete** — Steps: select-key → confirm
3. **startup-script add** — Steps: name → source → content
4. **startup-script delete** — Steps: select-script → confirm
5. **volume create** — Already uses wizard-like flow, wrap in engine
6. **volume action** — Steps: select-volume → select-action → confirm → execute
7. **vm action** — Steps: select-instance → select-action → confirm → execute
8. **settings theme** (--select mode) — Steps: select-theme

### Commands to keep standalone (with Approach B hints):

- vm list
- ssh-key list
- startup-script list
- volume list
- volume trash

### Wizard layout for action commands:

```go
Layout: []wizard.ViewDef{
    {ID: "hints", View: wizard.NewHintBarView()},
}
```

### Wizard layout for create commands (with progress):

```go
Layout: []wizard.ViewDef{
    {ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
    {ID: "hints",    View: wizard.NewHintBarView()},
}
```

## Execution Order

1. verdagostack: Task 2 (add PromptType to StepChangedMsg)
2. verdagostack: Task 1 (HintBarView)
3. verdagostack: Task 3 (component-level hints for standalone)
4. verdagostack: Release (e.g. v1.2.0)
5. verda-cli: Add HintBarView to existing wizard layouts (vm create, auth login)
6. verda-cli: Convert action commands to wizard flows
7. verda-cli: Convert add/delete commands to wizard flows
8. verda-cli: Update verda-cli dep, remove replace
