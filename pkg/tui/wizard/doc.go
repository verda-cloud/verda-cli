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

// Package wizard provides a TUI wizard engine for interactive CLI flows.
//
// # Overview
//
// A wizard Flow is an ordered list of Steps executed sequentially. Each step
// collects one value from the user via a prompt (select, text input, confirm,
// password, or multi-select). The engine handles skip logic, back navigation,
// validation, default values, and dependency-driven choice loading.
//
// The engine uses a composable view/actor layout. Each visual view is an
// independent actor with its own mailbox — it receives typed messages and
// renders independently. The engine acts as a message bus, broadcasting
// lifecycle events and routing inter-view messages.
//
// # Quick Start
//
//	var region string
//
//	flow := &wizard.Flow{
//	    Name: "create-vm",
//	    Steps: []wizard.Step{
//	        {
//	            Name:     "region",
//	            Prompt:   wizard.SelectPrompt,
//	            Required: true,
//	            Loader:   wizard.StaticChoices(
//	                wizard.Choice{Label: "Finland", Value: "FIN-01"},
//	                wizard.Choice{Label: "Sweden",  Value: "SWE-01"},
//	            ),
//	            Setter: func(v any) { region = v.(string) },
//	        },
//	    },
//	}
//
//	engine := wizard.NewEngine(prompter, status)
//	if err := engine.Run(ctx, flow); err != nil {
//	    return err
//	}
//	fmt.Println("Selected:", region)
//
// # Engine Constructor
//
// NewEngine takes a [tui.Prompter] for collecting input and an optional
// [tui.Status] for spinners/progress during async operations:
//
//	// Minimal — no spinners, default progress bar.
//	engine := wizard.NewEngine(prompter, nil)
//
//	// With status support and custom output.
//	engine := wizard.NewEngine(prompter, status, wizard.WithOutput(os.Stderr))
//
// The bubbletea.Prompter implements both tui.Prompter and tui.Status, so you
// can pass the same object for both:
//
//	p := bubbletea.New()
//	engine := wizard.NewEngine(p, p)
//
// # Steps
//
// Each Step defines what to collect and how:
//
//	wizard.Step{
//	    Name:        "instance-type",          // unique key in collected map
//	    Description: "Select instance type",   // shown in the prompt
//	    Prompt:      wizard.SelectPrompt,       // widget type
//	    Required:    true,                      // enforce non-empty
//	    DependsOn:   []string{"region"},        // invalidate when region changes
//
//	    // Loader fetches choices. Receives Status for spinners and Store for shared data.
//	    Loader: func(ctx context.Context, p tui.Prompter, s tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
//	        c := store.Collected()
//	        region := c["region"].(string)
//
//	        sp, _ := s.Spinner(ctx, "Loading instance types...")
//	        types, err := api.ListInstanceTypes(ctx, region)
//	        sp.Stop("Loaded")
//
//	        return buildChoices(types), err
//	    },
//
//	    // Default value based on previously collected values.
//	    Default: func(c map[string]any) any { return "standard-2vcpu" },
//
//	    // Validate before accepting.
//	    Validate: func(v any) error { return nil },
//
//	    // Setter writes to your options struct.
//	    Setter:   func(v any) { opts.InstanceType = v.(string) },
//	    Resetter: func()      { opts.InstanceType = "" },
//
//	    // Skip if already provided via CLI flag.
//	    IsSet: func() bool { return opts.InstanceType != "" },
//	    Value: func() any  { return opts.InstanceType },
//
//	    // Conditional skip based on earlier answers.
//	    ShouldSkip: func(c map[string]any) bool { return c["env"] == "dev" },
//	}
//
// # Prompt Types
//
//   - [SelectPrompt] — single choice from a list (arrow keys + type-to-filter)
//   - [MultiSelectPrompt] — multiple choices (space to toggle)
//   - [TextInputPrompt] — free-form text
//   - [ConfirmPrompt] — yes/no
//   - [PasswordPrompt] — masked input
//
// # Loaders
//
// Loaders fetch choices for select/multi-select steps. They receive four arguments:
//
//   - ctx — context for cancellation
//   - prompter — for running sub-prompts (e.g., "Create SSH key?")
//   - status — for showing spinners/progress during API calls (may be nil)
//   - store — shared data layer; use store.Collected() for step values
//
// For fixed choices, use the StaticChoices helper:
//
//	Loader: wizard.StaticChoices(
//	    wizard.Choice{Label: "Small",  Value: "small"},
//	    wizard.Choice{Label: "Large",  Value: "large"},
//	)
//
// For dynamic choices loaded from an API:
//
//	Loader: func(ctx context.Context, _ tui.Prompter, s tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
//	    c := store.Collected()
//	    region := c["region"].(string)
//
//	    // Show spinner while loading.
//	    sp, _ := s.Spinner(ctx, "Fetching sizes...")
//	    sizes, err := api.ListSizes(ctx, region)
//	    sp.Stop("Done")
//
//	    // Write to store — other regions can react to this.
//	    store.Set("cheapest", findCheapest(sizes))
//
//	    return toChoices(sizes), err
//	}
//
// # Dependencies
//
// When a step's DependsOn list includes another step name, the engine
// invalidates cached loader results when that dependency changes. This
// ensures choices are re-fetched when the user goes back and changes
// an earlier answer.
//
// # Back Navigation
//
// The engine automatically adds a "← Back" option to select/multi-select
// prompts when there is an editable prior step. Pressing Esc on any prompt
// also navigates back. The engine clears and resets all steps between the
// current position and the target.
//
// # Store
//
// The Store is a shared data layer accessible to loaders and regions:
//
//	store.Collected()              // snapshot of step name → value
//	store.Get("cost")              // read arbitrary data
//	store.Set("cost", 3.50)        // write arbitrary data
//
// Collected values are managed by the engine (set on step completion,
// cleared on back navigation). Arbitrary data is managed by your code —
// loaders can write values that custom views display.
//
// After the flow completes, read results via:
//
//	engine.Collected()             // same as store.Collected()
//	engine.Store().Get("cost")     // arbitrary data written during the flow
//
// # Layout and Views (Actor Model)
//
// The engine uses a composable actor model for rendering. The terminal is a
// vertical stack of views, each an independent actor with its own mailbox.
// The engine acts as a message bus: it broadcasts lifecycle events and routes
// inter-view messages.
//
// Default layout (when Flow.Layout is nil):
//
//	┌─────────────────────────────────────────────┐
//	│ ████████████████░░░░░░░░░░  Step 3 of 10   │  ← ProgressView (gradient bar)
//	│ ? Select instance type                      │  ← Prompt (engine's sequential loop)
//	│   > Standard-2vCPU-8GB                      │
//	│     Standard-4vCPU-16GB                     │
//	└─────────────────────────────────────────────┘
//
// # How the Actor Model Works
//
// Each view is an actor. It receives messages, updates its internal state,
// and returns a render string. The engine only prints a view's output when
// it actually changes, so redundant renders are never written to the terminal.
//
// The message flow for one step:
//
//	Engine                         Views
//	──────                         ─────
//	1. Broadcast StepChangedMsg ──→ ProgressView: renders "Step 3 of 10"  (changed → print)
//	                            ──→ CostView: no change                   (skip)
//	2. Run prompt (blocks for user input)
//	3. User picks a value
//	4. Broadcast CollectedChanged ─→ ProgressView: same output            (unchanged → skip)
//	                              ─→ CostView: recalculates "$3.20/hr"    (changed → print)
//	5. Advance to next step
//
// Key design properties:
//
//   - Views are decoupled: a CostView doesn't know about ProgressView.
//     They communicate through typed messages, not direct calls.
//   - Only changed output is printed: the engine tracks each view's last
//     printed output and skips unchanged views. This prevents duplicate
//     renders (e.g., ProgressView re-printing the same bar after a value
//     change that doesn't affect it).
//   - Views can publish messages to each other. The engine routes published
//     messages to subscribers, enabling view-to-view communication.
//
// # View Interface
//
// A view implements two methods:
//
//	type View interface {
//	    Update(msg any) (render string, publish []any)
//	    Subscribe() []reflect.Type
//	}
//
// Update receives a message and returns:
//   - render: the new display string (return the same string if nothing changed)
//   - publish: optional messages to broadcast to other views (nil if none)
//
// Subscribe returns the message types this view listens to. Return nil to
// receive all engine broadcasts. Return specific types to also receive
// inter-view messages of those types.
//
// # Built-in Messages
//
// The engine broadcasts these messages at specific points in the flow:
//
//   - [StepChangedMsg] — broadcast BEFORE each prompt. Contains Current
//     (1-based step position), Total (number of steps), StepName, and a
//     snapshot of Collected values. Use this for progress indicators.
//   - [CollectedChangedMsg] — broadcast AFTER a step completes. Contains
//     Key (step name), Value (selected value), and the updated Collected
//     snapshot. Use this for reactive displays (cost, summary, etc.).
//   - [StoreChangedMsg] — broadcast when a loader or view calls
//     store.Set(). Contains Key and Value. Use this for displays driven
//     by arbitrary data written during loading.
//
// # Progress Bar
//
// The built-in [ProgressView] uses the charmbracelet/bubbles progress
// component for gradient-colored rendering (same style as the bubbletea
// static progress example). It responds to [StepChangedMsg] and shows
// "Step X of Y" by default. Hidden for single-step flows.
//
// Default (bubbles default gradient #5A56E0 → #EE6FF8, step label):
//
//	wizard.NewProgressView()
//
// Custom gradient to match your theme:
//
//	wizard.NewProgressView(
//	    wizard.WithProgressGradient("#bd93f9", "#ff79c6"),
//	)
//
// Percentage mode (animated progress example style):
//
//	wizard.NewProgressView(wizard.WithProgressPercent())
//
// Solid fill, custom width:
//
//	wizard.NewProgressView(
//	    wizard.WithProgressSolidFill("#50fa7b"),
//	    wizard.WithProgressWidth(30),
//	)
//
// # Custom Layout
//
// Custom layout with additional views:
//
//	flow := &wizard.Flow{
//	    Layout: []wizard.ViewDef{
//	        {ID: "progress", View: wizard.NewProgressView(
//	            wizard.WithProgressGradient("#bd93f9", "#ff79c6"),
//	        )},
//	        {ID: "cost", View: &CostView{}},
//	    },
//	    Steps: []wizard.Step{...},
//	}
//
// # Custom View Example
//
// A view that shows estimated cost, updating when collected values change:
//
//	type CostView struct {
//	    last string
//	}
//
//	func (r *CostView) Update(msg any) (string, []any) {
//	    if m, ok := msg.(wizard.CollectedChangedMsg); ok {
//	        if price, ok := calculatePrice(m.Collected); ok {
//	            r.last = fmt.Sprintf("  Estimated cost: $%.2f/hr\n", price)
//	        }
//	    }
//	    return r.last, nil  // unchanged output → engine skips printing
//	}
//
//	func (r *CostView) Subscribe() []reflect.Type {
//	    return nil // receive all engine broadcasts
//	}
//
// # Inter-View Messaging
//
// Views can publish messages that other views subscribe to. The engine
// routes published messages to subscribers only (not broadcast to all).
//
// How it works:
//
//  1. The bus calls Update() on each view synchronously, in registration order.
//  2. Each view receives the message, checks the Go struct type, and processes
//     only the types it cares about (ignoring the rest by returning unchanged output).
//  3. If a view returns published messages, the bus routes them to subscribers
//     by matching the Go struct type against each view's Subscribe() list.
//
// There are no queues or channels — delivery is synchronous and immediate.
//
// # Message Ownership
//
// Message types are defined by the producer — the view that publishes the data:
//
//	// CostView defines and publishes CostUpdatedMsg.
//	type CostUpdatedMsg struct {
//	    Region string
//	    Price  float64
//	}
//
//	func (v *CostView) Update(msg any) (string, []any) {
//	    if m, ok := msg.(wizard.CollectedChangedMsg); ok {
//	        price := lookupPrice(m.Collected)
//	        return v.render(price), []any{CostUpdatedMsg{Price: price}}
//	    }
//	    return v.last, nil
//	}
//
// Consumers subscribe to the producer's message type. They don't know which
// view produces it — they only know the struct type:
//
//	// QuotaView subscribes to CostUpdatedMsg (defined by CostView).
//	func (v *QuotaView) Subscribe() []reflect.Type {
//	    return []reflect.Type{reflect.TypeFor[CostUpdatedMsg]()}
//	}
//
//	func (v *QuotaView) Update(msg any) (string, []any) {
//	    if m, ok := msg.(CostUpdatedMsg); ok {
//	        return fmt.Sprintf("  Region: %s ($%.2f/hr)\n", m.Region, m.Price), nil
//	    }
//	    return v.last, nil
//	}
//
// If multiple views produce different data that a consumer needs, the consumer
// subscribes to each type and combines them:
//
//	func (v *SummaryView) Subscribe() []reflect.Type {
//	    return []reflect.Type{
//	        reflect.TypeFor[CostUpdatedMsg](),  // from CostView
//	        reflect.TypeFor[QuotaUpdatedMsg](), // from QuotaView
//	    }
//	}
//
//	func (v *SummaryView) Update(msg any) (string, []any) {
//	    switch m := msg.(type) {
//	    case CostUpdatedMsg:
//	        v.price = m.Price
//	    case QuotaUpdatedMsg:
//	        v.remaining = m.Remaining
//	    }
//	    return v.render(), nil
//	}
//
// Messages chain: if a view publishes messages in response to a received
// message, those are delivered to their subscribers in the same cycle.
//
// See the wizard-views example for a complete working demonstration:
//
//	go run ./pkg/tui/examples/wizard-views
//
// # Pager
//
// For displaying long content (lists, logs, details), use [tui.Status.Pager]:
//
//	status.Pager(ctx, content)
//	status.Pager(ctx, content, tui.WithPagerTitle("Volumes in trash"))
//
// The pager auto-detects: if content fits the terminal, it prints directly.
// If it overflows, it shows an interactive scrollable viewport (alt-screen)
// with arrow keys/j/k/pgup/pgdn navigation and q/esc to exit.
//
// # Step Lifecycle
//
// Each step goes through one of these states:
//
//   - Pending — not yet visited
//   - Fixed — IsSet() returned true, value provided via flag/config (skipped)
//   - Skipped — ShouldSkip() returned true (skipped)
//   - AutoSkipped — loader returned empty choices for optional step (skipped)
//   - Completed — user answered or default applied
//
// Only Completed and Fixed steps appear in Collected(). The progress bar
// uses absolute step position (Step 3 of 12), so the total is stable and
// the bar always advances — even when steps are skipped.
package wizard
