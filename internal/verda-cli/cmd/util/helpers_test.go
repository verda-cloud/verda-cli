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

package util

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitesting "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

// The predicates in helpers.go are the single classification point main.go
// uses to map a user cancel to a silent exit 0. The wizard engine returns
// wrapping sentinels (ErrCancelled + low-level cause); this matrix pins that
// every shape of clean cancel classifies exactly one way, and real failures
// do not classify at all.
func TestPromptCancelClassification(t *testing.T) {
	stubFlow := func() *wizard.Flow {
		return &wizard.Flow{
			Name:  "test",
			Steps: []wizard.Step{{Name: "only", Prompt: wizard.TextInputPrompt, Required: true}},
		}
	}
	runWizard := func(result wizard.TestResult) error {
		engine := wizard.NewEngine(tuitesting.New(), nil, wizard.WithTestResults(result))
		return engine.Run(context.Background(), stubFlow())
	}

	ctrlC := runWizard(wizard.ExitResult())
	escFirst := runWizard(wizard.BackResult())
	boom := errors.New("api unreachable")

	tests := []struct {
		name                    string
		err                     error
		wantInterrupt, wantBack bool
	}{
		{"prompter Ctrl+C", tui.ErrInterrupted, true, false},
		{"prompter Esc", context.Canceled, false, true},
		{"wizard Ctrl+C", ctrlC, true, false},
		{"wizard Esc at first step", escFirst, false, true},
		// Ctrl+C during a wizard loader arrives wrapped in the step prefix.
		{"wizard loader ctx cancel", fmt.Errorf("step %q: %w", "x", context.Canceled), false, true},
		{"real failure", boom, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPromptInterrupt(tt.err); got != tt.wantInterrupt {
				t.Errorf("IsPromptInterrupt(%v) = %v, want %v", tt.err, got, tt.wantInterrupt)
			}
			if got := IsPromptBack(tt.err); got != tt.wantBack {
				t.Errorf("IsPromptBack(%v) = %v, want %v", tt.err, got, tt.wantBack)
			}
			wantCancel := tt.wantInterrupt || tt.wantBack
			if got := IsPromptCancel(tt.err); got != wantCancel {
				t.Errorf("IsPromptCancel(%v) = %v, want %v", tt.err, got, wantCancel)
			}
		})
	}
}
