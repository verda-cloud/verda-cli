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

package wizard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verda-cli/pkg/tui/bubbletea"
	tuitesting "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

// newTestEngine creates an engine with a resultOverride channel for unit testing.
// This bypasses the composite model — results are read directly from the channel.
func newTestEngine(results []promptResult, opts ...EngineOption) *Engine {
	p := tuitesting.New()
	e := NewEngine(p, nil, opts...)
	e.resultOverride = testResultCh(results...)
	return e
}

func TestEngine_HappyPath_AllStepsPrompted(t *testing.T) {
	var region, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:        "region",
				Description: "Select region",
				Prompt:      SelectPrompt,
				Required:    true,
				Loader: StaticChoices(
					Choice{Label: "Finland", Value: "FIN-01"},
					Choice{Label: "Sweden", Value: "SWE-01"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:        "gpu",
				Description: "Select GPU",
				Prompt:      SelectPrompt,
				Required:    true,
				DependsOn:   []string{"region"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["region"] == "FIN-01" {
						return []Choice{{Label: "H100", Value: "h100"}}, nil
					}
					return []Choice{{Label: "A100", Value: "a100"}}, nil
				},
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0), selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "FIN-01" {
		t.Errorf("expected region 'FIN-01', got %q", region)
	}
	if gpu != "h100" {
		t.Errorf("expected gpu 'h100', got %q", gpu)
	}
}

func TestEngine_SkipAlreadySet(t *testing.T) {
	var hostname string
	alreadySet := true

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "hostname",
				Prompt:   TextInputPrompt,
				Required: true,
				IsSet:    func() bool { return alreadySet },
				Setter:   func(v any) { hostname = v.(string) },
			},
		},
	}

	engine := newTestEngine(nil) // no prompting needed
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hostname != "" {
		t.Error("hostname should not be set — step was skipped")
	}
}

func TestEngine_ShouldSkip(t *testing.T) {
	var contract string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Spot", Value: "spot"}),
				Setter:   func(v any) {},
			},
			{
				Name:       "contract",
				Prompt:     SelectPrompt,
				Required:   false,
				ShouldSkip: func(c map[string]any) bool { return c["category"] == "spot" },
				Loader:     StaticChoices(Choice{Label: "Monthly", Value: "monthly"}),
				Setter:     func(v any) { contract = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contract != "" {
		t.Error("contract should be empty — step was skipped for spot")
	}
}

func TestEngine_TextInput(t *testing.T) {
	var hostname string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "hostname",
				Prompt:   TextInputPrompt,
				Required: true,
				Default:  func(_ map[string]any) any { return "my-vm-001" },
				Setter:   func(v any) { hostname = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{textResult("custom-host")})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hostname != "custom-host" {
		t.Errorf("expected 'custom-host', got %q", hostname)
	}
}

func TestEngine_MultiSelect(t *testing.T) {
	var keys []string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "ssh-keys",
				Prompt:   MultiSelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "key-a", Value: "id-a"},
					Choice{Label: "key-b", Value: "id-b"},
					Choice{Label: "key-c", Value: "id-c"},
				),
				Setter: func(v any) { keys = v.([]string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{multiSelectResult([]int{0, 2})})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "id-a" || keys[1] != "id-c" {
		t.Errorf("expected [id-a, id-c], got %v", keys)
	}
}

func TestEngine_Confirm(t *testing.T) {
	var isSpot bool

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "spot",
				Prompt:   ConfirmPrompt,
				Required: true,
				Setter:   func(v any) { isSpot = v.(bool) },
			},
		},
	}

	engine := newTestEngine([]promptResult{confirmResult(true)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isSpot {
		t.Error("expected isSpot to be true")
	}
}

func TestEngine_Password(t *testing.T) {
	var token string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "token",
				Prompt:   PasswordPrompt,
				Required: true,
				Setter:   func(v any) { token = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{passwordResult("secret-123")})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "secret-123" {
		t.Errorf("expected 'secret-123', got %q", token)
	}
}

func TestEngine_DefaultUsedForOptional(t *testing.T) {
	var desc string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "description",
				Prompt:   TextInputPrompt,
				Required: false,
				Default:  func(_ map[string]any) any { return "auto-generated" },
				Setter:   func(v any) { desc = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{textResult("")})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc != "auto-generated" {
		t.Errorf("expected 'auto-generated', got %q", desc)
	}
}

func TestEngine_BackNavigation_EmptyRequired(t *testing.T) {
	var region, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Finland", Value: "FIN-01"},
					Choice{Label: "Sweden", Value: "SWE-01"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["region"] == "FIN-01" {
						return []Choice{}, nil // empty — triggers auto-back
					}
					return []Choice{{Label: "A100", Value: "a100"}}, nil
				},
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	// First: select FIN-01 (idx 0) → gpu empty → auto-back
	// Second: select SWE-01 (idx 1) → gpu has A100 → select idx 0
	engine := newTestEngine([]promptResult{
		selectResult(0), // region: Finland
		selectResult(1), // region: Sweden (after auto-back)
		selectResult(0), // gpu: A100
	})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "SWE-01" {
		t.Errorf("expected region 'SWE-01', got %q", region)
	}
	if gpu != "a100" {
		t.Errorf("expected gpu 'a100', got %q", gpu)
	}
}

func TestEngine_EmptyRequired_AtFirstStep_ReturnsError(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "gpu",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil)
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error when first step has no options")
	}
}

// Regression: transition() used to zero rewindCount on every reset, so the
// auto-rewind guard never fired — a step whose loader always returns empty
// choices bounced the user back to the prior step forever (only Ctrl+C
// escaped). The guard must trip after maxRewindsPerStep attempts whether the
// step rewinds via a declared DependsOn or the rewindOne fallback.
func TestEngine_EmptyChoices_RewindGuardTerminates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dependsOn []string
	}{
		{name: "declared dependency", dependsOn: []string{"region"}},
		{name: "no dependency (rewindOne fallback)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loaderCalls := 0
			flow := &Flow{
				Name: "test",
				Steps: []Step{
					{
						Name:     "region",
						Prompt:   SelectPrompt,
						Required: true,
						Loader:   StaticChoices(Choice{Label: "Finland", Value: "FIN-01"}),
						Setter:   func(v any) {},
					},
					{
						Name:      "gpu",
						Prompt:    SelectPrompt,
						Required:  true,
						DependsOn: tc.dependsOn,
						Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
							loaderCalls++
							if loaderCalls > maxRewindsPerStep+2 {
								// Breaks the loop if the guard regresses, so the
								// test fails on the error instead of hanging.
								return nil, fmt.Errorf("loader called %d times: rewind guard did not trip", loaderCalls)
							}
							return []Choice{}, nil
						},
						Setter: func(v any) {},
					},
				},
			}

			// One selectResult per re-ask of the region step; the guard must
			// trip before the loader safety break kicks in.
			engine := newTestEngine([]promptResult{
				selectResult(0), selectResult(0), selectResult(0), selectResult(0), selectResult(0),
			}, WithOutput(io.Discard))
			err := engine.Run(context.Background(), flow)
			if err == nil || !strings.Contains(err.Error(), "no options available after") {
				t.Fatalf("expected rewind guard error, got %v", err)
			}
			if loaderCalls != maxRewindsPerStep+1 {
				t.Errorf("expected %d loader calls before guard trips, got %d", maxRewindsPerStep+1, loaderCalls)
			}
		})
	}
}

func TestEngine_ValidationError_RepromptsUntilValid(t *testing.T) {
	var size string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "size",
				Prompt:   TextInputPrompt,
				Required: true,
				Validate: func(v any) error {
					if v.(string) == "bad" {
						return fmt.Errorf("invalid size")
					}
					return nil
				},
				Setter: func(v any) { size = v.(string) },
			},
		},
	}

	// First: "bad" (fails validation, re-prompt), second: "100" (passes)
	engine := newTestEngine([]promptResult{
		textResult("bad"),
		textResult("100"),
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != "100" {
		t.Errorf("expected '100', got %q", size)
	}
}

func TestEngine_CachesLoaderResults(t *testing.T) {
	loadCount := 0

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "static",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					loadCount++
					return []Choice{{Label: "A", Value: "a"}}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loadCount != 1 {
		t.Errorf("expected loader called once, got %d", loadCount)
	}
}

func TestEngine_FullFlow_VMCreate(t *testing.T) {
	var category, contract, compute, instType, location, image, hostname string
	var sshKeys []string

	flow := &Flow{
		Name: "vm-create",
		Steps: []Step{
			{
				Name:     "instance-category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "On-Demand", Value: "on-demand"},
					Choice{Label: "Spot", Value: "spot"},
				),
				Setter: func(v any) { category = v.(string) },
			},
			{
				Name:       "contract",
				Prompt:     SelectPrompt,
				Required:   false,
				Default:    func(_ map[string]any) any { return "PAY_AS_YOU_GO" },
				ShouldSkip: func(c map[string]any) bool { return c["instance-category"] == "spot" },
				Loader: StaticChoices(
					Choice{Label: "Pay as you go", Value: "PAY_AS_YOU_GO"},
					Choice{Label: "1 month", Value: "1_MONTH"},
				),
				Setter: func(v any) { contract = v.(string) },
			},
			{
				Name:     "compute-category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "GPU", Value: "GPU"},
					Choice{Label: "CPU", Value: "CPU"},
				),
				Setter: func(v any) { compute = v.(string) },
			},
			{
				Name:      "instance-type",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"compute-category"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["compute-category"] == "GPU" {
						return []Choice{
							{Label: "H100 80GB", Value: "1H100"},
							{Label: "A100 40GB", Value: "1A100"},
						}, nil
					}
					return []Choice{{Label: "32 vCPU", Value: "32CPU"}}, nil
				},
				Setter: func(v any) { instType = v.(string) },
			},
			{
				Name:      "location",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"instance-type"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{{Label: "Finland (FIN-01)", Value: "FIN-01"}}, nil
				},
				Setter: func(v any) { location = v.(string) },
			},
			{
				Name:      "image",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"instance-type"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{{Label: "Ubuntu 24.04", Value: "ubuntu-24.04"}}, nil
				},
				Setter: func(v any) { image = v.(string) },
			},
			{
				Name:     "ssh-keys",
				Prompt:   MultiSelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "my-key", Value: "key-1"},
					Choice{Label: "work-key", Value: "key-2"},
				),
				Setter: func(v any) { sshKeys = v.([]string) },
			},
			{
				Name:     "hostname",
				Prompt:   TextInputPrompt,
				Required: true,
				Default:  func(_ map[string]any) any { return "vm-001" },
				Setter:   func(v any) { hostname = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0),                // on-demand
		selectResult(0),                // pay as you go
		selectResult(0),                // GPU
		selectResult(0),                // H100
		selectResult(0),                // FIN-01
		selectResult(0),                // Ubuntu
		multiSelectResult([]int{0, 1}), // both SSH keys
		textResult("my-vm"),            // hostname
	})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if category != "on-demand" {
		t.Errorf("category: got %q", category)
	}
	if contract != "PAY_AS_YOU_GO" {
		t.Errorf("contract: got %q", contract)
	}
	if compute != "GPU" {
		t.Errorf("compute: got %q", compute)
	}
	if instType != "1H100" {
		t.Errorf("instType: got %q", instType)
	}
	if location != "FIN-01" {
		t.Errorf("location: got %q", location)
	}
	if image != "ubuntu-24.04" {
		t.Errorf("image: got %q", image)
	}
	if len(sshKeys) != 2 {
		t.Errorf("sshKeys: got %v", sshKeys)
	}
	if hostname != "my-vm" {
		t.Errorf("hostname: got %q", hostname)
	}
}

func TestEngine_UserInitiatedBack(t *testing.T) {
	var region, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Finland", Value: "FIN-01"},
					Choice{Label: "Sweden", Value: "SWE-01"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region"},
				Loader: StaticChoices(
					Choice{Label: "H100", Value: "h100"},
					Choice{Label: "A100", Value: "a100"},
				),
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	// Step 1: select Finland (idx 0)
	// Step 2: select "← Back" (idx 2 = last, after h100 and a100)
	// Step 1 again: select Sweden (idx 1)
	// Step 2 again: select A100 (idx 1)
	engine := newTestEngine([]promptResult{
		selectResult(0), // region: Finland
		selectResult(2), // gpu: ← Back
		selectResult(1), // region: Sweden
		selectResult(1), // gpu: A100
	})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "SWE-01" {
		t.Errorf("expected region 'SWE-01', got %q", region)
	}
	if gpu != "a100" {
		t.Errorf("expected gpu 'a100', got %q", gpu)
	}
}

func TestEngine_EscBack(t *testing.T) {
	// Esc (ActionBack) should navigate to the prior editable step.
	var region, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Finland", Value: "FIN-01"},
					Choice{Label: "Sweden", Value: "SWE-01"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:     "gpu",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "H100", Value: "h100"},
					Choice{Label: "A100", Value: "a100"},
				),
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // region: Finland
		backResult(),    // gpu: Esc → back to region
		selectResult(1), // region: Sweden
		selectResult(1), // gpu: A100
	})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "SWE-01" {
		t.Errorf("expected region 'SWE-01', got %q", region)
	}
	if gpu != "a100" {
		t.Errorf("expected gpu 'a100', got %q", gpu)
	}
}

func TestEngine_EscOnFirstStep_Cancels(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "first",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				Setter:   func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{backResult()})
	err := engine.Run(context.Background(), flow)
	// Esc on the first step must classify as a soft cancel (context.Canceled)
	// under the ErrCancelled umbrella — cmdutil.IsPromptBack/Cancel key on it.
	if !errors.Is(err, ErrCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected Esc cancel (ErrCancelled wrapping context.Canceled), got %v", err)
	}
	if errors.Is(err, tui.ErrInterrupted) {
		t.Fatal("Esc cancel must not classify as Ctrl+C (tui.ErrInterrupted)")
	}
}

func TestEngine_CtrlC_Exits(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "first",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				Setter:   func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{exitResult()})
	err := engine.Run(context.Background(), flow)
	// Ctrl+C must classify as a hard interrupt (tui.ErrInterrupted) under the
	// ErrCancelled umbrella — cmdutil.IsPromptInterrupt/Cancel key on it.
	if !errors.Is(err, ErrCancelled) || !errors.Is(err, tui.ErrInterrupted) {
		t.Fatalf("expected Ctrl+C cancel (ErrCancelled wrapping tui.ErrInterrupted), got %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("Ctrl+C cancel must not classify as soft back (context.Canceled)")
	}
}

func TestEngine_BackNotShownOnFirstStep(t *testing.T) {
	var val string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "first",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "A", Value: "a"},
					Choice{Label: "B", Value: "b"},
				),
				Setter: func(v any) { val = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "a" {
		t.Errorf("expected 'a', got %q", val)
	}
}

func TestEngine_RequiredTextInput_RepromptsOnEmpty(t *testing.T) {
	var name string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "name",
				Prompt:   TextInputPrompt,
				Required: true,
				Setter:   func(v any) { name = v.(string) },
			},
		},
	}

	// First: empty (re-prompt), second: valid
	engine := newTestEngine([]promptResult{
		textResult(""),
		textResult("my-service"),
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "my-service" {
		t.Errorf("expected 'my-service', got %q", name)
	}
}

func TestEngine_DependencyAwareAutoBack(t *testing.T) {
	var region, name, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Region A", Value: "a"},
					Choice{Label: "Region B", Value: "b"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:     "name",
				Prompt:   TextInputPrompt,
				Required: true,
				Setter:   func(v any) { name = v.(string) },
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["region"] == "a" {
						return []Choice{}, nil // empty for region A
					}
					return []Choice{{Label: "H100", Value: "h100"}}, nil
				},
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0),     // region: A
		textResult("test"),  // name
		selectResult(1),     // region: B (after dependency-aware auto-back)
		textResult("test2"), // name again
		selectResult(0),     // gpu: H100
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "b" {
		t.Errorf("expected region 'b', got %q", region)
	}
	if name != "test2" {
		t.Errorf("expected name 'test2', got %q", name)
	}
	if gpu != "h100" {
		t.Errorf("expected gpu 'h100', got %q", gpu)
	}
}

func TestEngine_NilSetter_NoPanic(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "temp",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				// Setter intentionally nil — should not panic
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if engine.Collected()["temp"] != "a" {
		t.Errorf("expected collected value 'a', got %v", engine.Collected()["temp"])
	}
}

func TestEngine_ResetterCalledOnBack(t *testing.T) {
	var category, contract string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "On-Demand", Value: "on-demand"},
					Choice{Label: "Spot", Value: "spot"},
				),
				Setter:   func(v any) { category = v.(string) },
				Resetter: func() { category = "" },
			},
			{
				Name:       "contract",
				Prompt:     SelectPrompt,
				Required:   false,
				Default:    func(_ map[string]any) any { return "PAY_AS_YOU_GO" },
				ShouldSkip: func(c map[string]any) bool { return c["category"] == "spot" },
				Loader:     StaticChoices(Choice{Label: "Monthly", Value: "monthly"}),
				Setter:     func(v any) { contract = v.(string) },
				Resetter:   func() { contract = "" },
			},
		},
	}

	// First run: on-demand → monthly
	engine := newTestEngine([]promptResult{
		selectResult(0), // category: on-demand
		selectResult(0), // contract: monthly
	})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contract != "monthly" {
		t.Errorf("expected 'monthly', got %q", contract)
	}

	// Second run: on-demand, then go back from contract, pick spot → contract skipped & reset
	contract = "stale-value"
	engine2 := newTestEngine([]promptResult{
		selectResult(0), // category: on-demand
		selectResult(1), // contract: ← Back
		selectResult(1), // category: spot (contract will be skipped)
	})
	err = engine2.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if category != "spot" {
		t.Errorf("expected 'spot', got %q", category)
	}
	if _, exists := engine2.Collected()["contract"]; exists {
		t.Error("contract should not be in collected — step was skipped")
	}
}

func TestEngine_SkipClearsStaleValue(t *testing.T) {
	var contract string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "On-Demand", Value: "on-demand"},
					Choice{Label: "Spot", Value: "spot"},
				),
				Setter: func(v any) {},
			},
			{
				Name:       "contract",
				Prompt:     SelectPrompt,
				Required:   false,
				ShouldSkip: func(c map[string]any) bool { return c["category"] == "spot" },
				Loader:     StaticChoices(Choice{Label: "Monthly", Value: "monthly"}),
				Setter:     func(v any) { contract = v.(string) },
				Resetter:   func() { contract = "" },
			},
			{
				Name:     "done",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "OK", Value: "ok"}),
				Setter:   func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // category: on-demand
		selectResult(0), // contract: monthly
		selectResult(1), // done: ← Back
		selectResult(1), // contract: ← Back
		selectResult(1), // category: spot (contract skipped, resetter called)
		selectResult(0), // done: OK
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contract != "" {
		t.Errorf("expected contract reset to empty, got %q", contract)
	}
}

func TestEngine_FixedDependency_ReturnsError(t *testing.T) {
	fixedRegion := true

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				IsSet:    func() bool { return fixedRegion },
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				Setter:   func(v any) {},
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error for fixed dependency with empty choices")
	}
}

func TestEngine_OptionalEmptyChoices_SkipsWithDefault(t *testing.T) {
	var addon string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "addon",
				Prompt:   SelectPrompt,
				Required: false,
				Default:  func(_ map[string]any) any { return "none" },
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) { addon = v.(string) },
			},
		},
	}

	engine := newTestEngine(nil)
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addon != "none" {
		t.Errorf("expected 'none', got %q", addon)
	}
}

func TestEngine_IsSetPropagatesValueToCollected(t *testing.T) {
	var gpu string
	fixedRegion := "FIN-01"

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "region",
				Prompt: SelectPrompt,
				IsSet:  func() bool { return true },
				Value:  func() any { return fixedRegion },
				Loader: StaticChoices(Choice{Label: "Finland", Value: "FIN-01"}),
				Setter: func(v any) {},
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["region"] != "FIN-01" {
						t.Errorf("expected region 'FIN-01' in collected, got %v", c["region"])
					}
					return []Choice{{Label: "H100", Value: "h100"}}, nil
				},
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)})
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gpu != "h100" {
		t.Errorf("expected gpu 'h100', got %q", gpu)
	}
	if engine.Collected()["region"] != "FIN-01" {
		t.Errorf("expected region in collected, got %v", engine.Collected()["region"])
	}
}

func TestEngine_MixedFixedEditableDependencies(t *testing.T) {
	var category, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "region",
				Prompt: SelectPrompt,
				IsSet:  func() bool { return true },
				Value:  func() any { return "FIN-01" },
				Setter: func(v any) {},
			},
			{
				Name:     "category",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "GPU", Value: "GPU"},
					Choice{Label: "CPU", Value: "CPU"},
				),
				Setter: func(v any) { category = v.(string) },
			},
			{
				Name:      "gpu",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"region", "category"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if c["category"] == "GPU" {
						return []Choice{}, nil
					}
					return []Choice{{Label: "32 vCPU", Value: "32cpu"}}, nil
				},
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // category: GPU
		selectResult(1), // category: CPU (after auto-back)
		selectResult(0), // gpu: 32cpu
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if category != "CPU" {
		t.Errorf("expected category 'CPU', got %q", category)
	}
	if gpu != "32cpu" {
		t.Errorf("expected gpu '32cpu', got %q", gpu)
	}
}

func TestEngine_OptionalEmptyNoDefault_Resets(t *testing.T) {
	addon := "stale"

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "addon",
				Prompt:   SelectPrompt,
				Required: false,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter:   func(v any) { addon = v.(string) },
				Resetter: func() { addon = "" },
			},
		},
	}

	engine := newTestEngine(nil)
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addon != "" {
		t.Errorf("expected addon reset to empty, got %q", addon)
	}
}

func TestEngine_SkippedDepNotPickedByAutoBack(t *testing.T) {
	var a, c string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "a",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "X", Value: "x"},
					Choice{Label: "Y", Value: "y"},
				),
				Setter: func(v any) { a = v.(string) },
			},
			{
				Name:       "b",
				Prompt:     SelectPrompt,
				Required:   true,
				DependsOn:  []string{"a"},
				ShouldSkip: func(col map[string]any) bool { return col["a"] == "x" },
				Loader:     StaticChoices(Choice{Label: "B1", Value: "b1"}),
				Setter:     func(v any) {},
			},
			{
				Name:      "c",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"b"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					col := store.Collected()
					if col["b"] == nil {
						return []Choice{}, nil
					}
					return []Choice{{Label: "C1", Value: "c1"}}, nil
				},
				Setter: func(v any) { c = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // A: X
		selectResult(1), // A: Y (after auto-back past skipped B)
		selectResult(0), // B: B1
		selectResult(0), // C: C1
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != "y" {
		t.Errorf("expected a='y', got %q", a)
	}
	if c != "c1" {
		t.Errorf("expected c='c1', got %q", c)
	}
}

func TestEngine_ShouldSkipOverridesIsSet(t *testing.T) {
	var tls bool
	tlsSet := true

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Dev", Value: "dev"}),
				Setter:   func(v any) {},
			},
			{
				Name:       "tls",
				Prompt:     ConfirmPrompt,
				Required:   true,
				ShouldSkip: func(c map[string]any) bool { return c["env"] == "dev" },
				IsSet:      func() bool { return tlsSet },
				Value:      func() any { return true },
				Setter:     func(v any) { tls = v.(bool) },
				Resetter:   func() { tls = false },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tls {
		t.Error("expected tls=false — ShouldSkip should override IsSet")
	}
	if _, exists := engine.Collected()["tls"]; exists {
		t.Error("tls should not be in collected when skipped")
	}
}

func TestEngine_BackNotShownWhenAllPriorFixed(t *testing.T) {
	var val string
	fixedFirst := true

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "fixed",
				Prompt: SelectPrompt,
				IsSet:  func() bool { return fixedFirst },
				Value:  func() any { return "pre-set" },
				Loader: StaticChoices(Choice{Label: "X", Value: "x"}),
				Setter: func(v any) {},
			},
			{
				Name:     "second",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				Setter:   func(v any) { val = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "a" {
		t.Errorf("expected 'a', got %q", val)
	}
}

func TestEngine_SkippedDepChainRewindsToController(t *testing.T) {
	var env, svcName string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Dev", Value: "dev"},
					Choice{Label: "Prod", Value: "prod"},
				),
				Setter: func(v any) { env = v.(string) },
			},
			{
				Name:     "svc-name",
				Prompt:   TextInputPrompt,
				Required: true,
				Setter:   func(v any) { svcName = v.(string) },
			},
			{
				Name:       "tls",
				Prompt:     ConfirmPrompt,
				Required:   true,
				ShouldSkip: func(c map[string]any) bool { return c["env"] == "dev" },
				Setter:     func(v any) {},
				Resetter:   func() {},
			},
			{
				Name:      "cert",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"tls"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					c := store.Collected()
					if _, hasTLS := c["tls"]; !hasTLS {
						return []Choice{}, nil
					}
					return []Choice{{Label: "wildcard.pem", Value: "wildcard"}}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0),      // env: dev
		textResult("myapp"),  // svc-name
		selectResult(1),      // env: prod (after auto-back to earliest editable = env)
		textResult("myapp2"), // svc-name again (was reset)
		confirmResult(true),  // tls: yes (not skipped for prod)
		selectResult(0),      // cert: wildcard
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != "prod" {
		t.Errorf("expected env='prod', got %q", env)
	}
	if svcName != "myapp2" {
		t.Errorf("expected svc-name='myapp2', got %q", svcName)
	}
}

func TestEngine_PresetValueValidated(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "replicas",
				Prompt: TextInputPrompt,
				IsSet:  func() bool { return true },
				Value:  func() any { return "bad" },
				Validate: func(v any) error {
					if v.(string) == "bad" {
						return fmt.Errorf("invalid replica count")
					}
					return nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected validation error for preset value")
	}
}

func TestEngine_RunClearsStateFromPreviousRun(t *testing.T) {
	var val string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "item",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}, Choice{Label: "B", Value: "b"}),
				Setter:   func(v any) { val = v.(string) },
			},
		},
	}

	// First run: select A
	engine := newTestEngine([]promptResult{selectResult(0)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if val != "a" {
		t.Errorf("run 1: expected 'a', got %q", val)
	}

	// Second run with same engine: select B
	engine.resultOverride = testResultCh(selectResult(1))
	err = engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if val != "b" {
		t.Errorf("run 2: expected 'b', got %q", val)
	}
	if engine.Collected()["item"] != "b" {
		t.Errorf("collected should be 'b', got %v", engine.Collected()["item"])
	}
}

func TestEngine_SkippedDepsNoRewindTarget_ReturnsError(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "fixed",
				Prompt: SelectPrompt,
				IsSet:  func() bool { return true },
				Value:  func() any { return "x" },
				Setter: func(v any) {},
			},
			{
				Name:       "skipped",
				Prompt:     SelectPrompt,
				ShouldSkip: func(c map[string]any) bool { return true },
				Loader:     StaticChoices(Choice{Label: "S", Value: "s"}),
				Setter:     func(v any) {},
			},
			{
				Name:      "broken",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"skipped"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error when no rewind target exists")
	}
}

func TestEngine_BackSkipsAutoSkippedOptional(t *testing.T) {
	var a, c string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "a",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "A1", Value: "a1"},
					Choice{Label: "A2", Value: "a2"},
				),
				Setter: func(v any) { a = v.(string) },
			},
			{
				Name:     "b",
				Prompt:   SelectPrompt,
				Required: false,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil // always empty → auto-skipped
				},
				Setter: func(v any) {},
			},
			{
				Name:     "c",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "C1", Value: "c1"},
					Choice{Label: "C2", Value: "c2"},
				),
				Setter: func(v any) { c = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // A: A1
		selectResult(2), // C: ← Back (skips past auto-skipped B to A)
		selectResult(1), // A: A2
		selectResult(0), // C: C1
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != "a2" {
		t.Errorf("expected a='a2', got %q", a)
	}
	if c != "c1" {
		t.Errorf("expected c='c1', got %q", c)
	}
}

func TestEngine_ConfirmDefaultHonored(t *testing.T) {
	var tls bool

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "tls",
				Prompt:   ConfirmPrompt,
				Required: true,
				Default:  func(_ map[string]any) any { return true },
				Setter:   func(v any) { tls = v.(bool) },
			},
		},
	}

	engine := newTestEngine([]promptResult{confirmResult(true)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tls {
		t.Error("expected tls=true")
	}
}

func TestEngine_AutoSkippedDepNotRewindTarget(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "addon",
				Prompt:   SelectPrompt,
				Required: false,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
			{
				Name:      "addon-config",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"addon"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error, not infinite loop")
	}
}

func TestEngine_NameFallbackWhenDescriptionEmpty(t *testing.T) {
	var val string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A", Value: "a"}),
				Setter:   func(v any) { val = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "a" {
		t.Errorf("expected 'a', got %q", val)
	}
}

func TestEngine_SelectDefaultForwarded(t *testing.T) {
	var val string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "size",
				Prompt:   SelectPrompt,
				Required: true,
				Default:  func(_ map[string]any) any { return "medium" },
				Loader: StaticChoices(
					Choice{Label: "Small", Value: "small"},
					Choice{Label: "Medium", Value: "medium"},
					Choice{Label: "Large", Value: "large"},
				),
				Setter: func(v any) { val = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(1)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "medium" {
		t.Errorf("expected 'medium', got %q", val)
	}
}

func TestEngine_GoBackClearsDownstreamCollected(t *testing.T) {
	var a, b, c string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "a",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "A1", Value: "a1"}, Choice{Label: "A2", Value: "a2"}),
				Setter:   func(v any) { a = v.(string) },
			},
			{
				Name:     "b",
				Prompt:   TextInputPrompt,
				Required: true,
				Setter:   func(v any) { b = v.(string) },
			},
			{
				Name:     "c",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "C1", Value: "c1"}),
				Setter:   func(v any) { c = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0),     // A: A1
		textResult("hello"), // B: hello
		selectResult(1),     // C: ← Back
		textResult("world"), // B: world
		selectResult(0),     // C: C1
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != "a1" {
		t.Errorf("expected a='a1', got %q", a)
	}
	if b != "world" {
		t.Errorf("expected b='world', got %q", b)
	}
	if c != "c1" {
		t.Errorf("expected c='c1', got %q", c)
	}
}

func TestEngine_SkippedDepWithFixedController_ErrorsNotLoops(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:   "env",
				Prompt: SelectPrompt,
				IsSet:  func() bool { return true },
				Value:  func() any { return "dev" },
				Setter: func(v any) {},
			},
			{
				Name:     "svc-name",
				Prompt:   TextInputPrompt,
				Required: true,
				Setter:   func(v any) {},
			},
			{
				Name:       "tls",
				Prompt:     ConfirmPrompt,
				Required:   true,
				ShouldSkip: func(c map[string]any) bool { return c["env"] == "dev" },
				Setter:     func(v any) {},
			},
			{
				Name:      "cert",
				Prompt:    SelectPrompt,
				Required:  true,
				DependsOn: []string{"tls"},
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return []Choice{}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{
		textResult("svc1"),
		textResult("svc2"),
		textResult("svc3"),
		textResult("svc4"),
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error, not infinite loop")
	}
}

func TestEngine_IsEditable_FixedVsIsSet(t *testing.T) {
	var region, env, gpu string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "Finland", Value: "FIN-01"},
					Choice{Label: "Sweden", Value: "SWE-01"},
				),
				Setter: func(v any) { region = v.(string) },
			},
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Prod", Value: "prod"}),
				Setter:   func(v any) { env = v.(string) },
				IsSet:    func() bool { return true },
				Value:    func() any { return "prod" },
			},
			{
				Name:     "gpu",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: StaticChoices(
					Choice{Label: "H100", Value: "h100"},
					Choice{Label: "A100", Value: "a100"},
				),
				Setter: func(v any) { gpu = v.(string) },
			},
		},
	}

	engine := newTestEngine([]promptResult{
		selectResult(0), // region: Finland
		selectResult(2), // gpu: ← Back (skips fixed env, goes to region)
		selectResult(1), // region: Sweden
		selectResult(1), // gpu: A100
	}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if region != "SWE-01" {
		t.Errorf("expected region 'SWE-01', got %q", region)
	}
	if env != "prod" {
		t.Errorf("expected env 'prod', got %q", env)
	}
	if gpu != "a100" {
		t.Errorf("expected gpu 'a100', got %q", gpu)
	}
}

func TestEngine_LoaderError_Propagated(t *testing.T) {
	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *Store) ([]Choice, error) {
					return nil, fmt.Errorf("connection failed")
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine(nil, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection failed") {
		t.Errorf("expected 'connection failed' in error, got %q", err)
	}
}

func TestEngine_LoaderReceivesStatusAndStore(t *testing.T) {
	var gotStore bool

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "a",
				Prompt:   SelectPrompt,
				Required: true,
				Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, store *Store) ([]Choice, error) {
					gotStore = store != nil
					store.Set("loaded", true)
					return []Choice{{Label: "X", Value: "x"}}, nil
				},
				Setter: func(v any) {},
			},
		},
	}

	engine := newTestEngine([]promptResult{selectResult(0)}, WithOutput(io.Discard))
	err := engine.Run(context.Background(), flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotStore {
		t.Error("loader should receive non-nil store")
	}
	v, ok := engine.Store().Get("loaded")
	if !ok || v != true {
		t.Error("loader should be able to write to store")
	}
}

// TestEngine_MultiSelectMinErrorPlumbed verifies that Step.MinError reaches
// the multiselect model built by the engine, replacing the default
// validation message for a Required multi-select.
func TestEngine_MultiSelectMinErrorPlumbed(t *testing.T) {
	engine := newTestEngine(nil)
	step := Step{
		Name:     "regions",
		Prompt:   MultiSelectPrompt,
		Required: true,
		MinError: func(min int) string { return fmt.Sprintf("choose %d+ region(s)", min) },
	}
	choices := []Choice{{Label: "us", Value: "us"}, {Label: "eu", Value: "eu"}}

	model := engine.buildPromptModel(step, choices, false)

	// Press Enter with nothing selected: Required enforces min=1, so the
	// confirm handler fires the (now custom) min-error message.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := updated.(bubbletea.PromptModel).View().Content

	if !strings.Contains(view, "choose 1+ region(s)") {
		t.Errorf("expected wizard MinError override in view, got %q", view)
	}
}
