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
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tuitesting "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

// keySequence builds a byte sequence from VT100 escape codes.
func keySequence(keys ...string) io.Reader {
	var buf bytes.Buffer
	for _, k := range keys {
		buf.WriteString(k)
	}
	return &buf
}

const (
	keyEnter = "\r"
	keyCtrlC = "\x03"
	keyDown  = "\x1b[B"
)

func TestIntegration_SelectAndComplete(t *testing.T) {
	var env string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Dev", Value: "dev"}, Choice{Label: "Prod", Value: "prod"}),
				Setter:   func(v any) { env = v.(string) },
			},
		},
	}

	// Enter selects first item
	input := keySequence(keyEnter)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != "dev" {
		t.Errorf("expected env 'dev', got %q", env)
	}
}

func TestIntegration_TwoStepFlow(t *testing.T) {
	var env, region string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Dev", Value: "dev"}, Choice{Label: "Prod", Value: "prod"}),
				Setter:   func(v any) { env = v.(string) },
			},
			{
				Name:     "region",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "US", Value: "us"}, Choice{Label: "EU", Value: "eu"}),
				Setter:   func(v any) { region = v.(string) },
			},
		},
	}

	// Enter (select first), Enter (select first)
	input := keySequence(keyEnter, keyEnter)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != "dev" {
		t.Errorf("expected env 'dev', got %q", env)
	}
	if region != "us" {
		t.Errorf("expected region 'us', got %q", region)
	}
}

func TestIntegration_CtrlC_Exits(t *testing.T) {
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
		},
	}

	input := keySequence(keyCtrlC)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err == nil || !strings.Contains(err.Error(), "wizard cancelled") {
		t.Fatalf("expected 'wizard cancelled', got %v", err)
	}
}

func TestIntegration_ArrowDownAndSelect(t *testing.T) {
	var env string

	flow := &Flow{
		Name: "test",
		Steps: []Step{
			{
				Name:     "env",
				Prompt:   SelectPrompt,
				Required: true,
				Loader:   StaticChoices(Choice{Label: "Dev", Value: "dev"}, Choice{Label: "Prod", Value: "prod"}),
				Setter:   func(v any) { env = v.(string) },
			},
		},
	}

	// Down, Enter (select second item)
	input := keySequence(keyDown, keyEnter)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env != "prod" {
		t.Errorf("expected env 'prod', got %q", env)
	}
}

// TestIntegration_CtrlC_ConfirmExit_SingleY exercises the
// WithExitConfirmation() happy path: Ctrl+C shows "Exit wizard?", user types
// 'y', wizard exits cleanly.
func TestIntegration_CtrlC_ConfirmExit_SingleY(t *testing.T) {
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
		},
	}

	input := keySequence(keyCtrlC, "y", keyEnter)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
		WithExitConfirmation(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err == nil || !strings.Contains(err.Error(), "wizard cancelled") {
		t.Fatalf("expected 'wizard cancelled', got %v", err)
	}
}

// TestIntegration_CtrlC_ConfirmExit_DoubleCtrlC verifies the
// "double-tap Ctrl+C force exits" behavior: hitting Ctrl+C twice must get
// the user out of the wizard even with WithExitConfirmation() enabled.
func TestIntegration_CtrlC_ConfirmExit_DoubleCtrlC(t *testing.T) {
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
		},
	}

	input := keySequence(keyCtrlC, keyCtrlC)
	engine := NewEngine(tuitesting.New(), nil,
		WithInput(input),
		WithOutput(io.Discard),
		WithExitConfirmation(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := engine.Run(ctx, flow)
	if err == nil || !strings.Contains(err.Error(), "wizard cancelled") {
		t.Fatalf("expected 'wizard cancelled' on double Ctrl+C, got %v", err)
	}
}
