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
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// Regression: a bare errors.New sentinel wraps nothing, so the CLI's cancel
// predicates (which key on tui.ErrInterrupted / context.Canceled) classified a
// clean wizard abort as a real failure — stderr noise and exit 1 on Ctrl+C.
// Every value Run returns for a user abort must carry both the umbrella
// sentinel and its low-level cause.
func TestCancelSentinels_CarryUmbrellaAndCause(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCause error
		notCause  error
	}{
		{
			name:      "Ctrl+C is a hard interrupt",
			err:       errCancelledInterrupt,
			wantCause: tui.ErrInterrupted,
			notCause:  context.Canceled,
		},
		{
			name:      "Esc with nowhere back is a soft cancel",
			err:       errCancelledBack,
			wantCause: context.Canceled,
			notCause:  tui.ErrInterrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, ErrCancelled) {
				t.Errorf("errors.Is(%v, ErrCancelled) = false; callers matching the umbrella sentinel break", tt.err)
			}
			if !errors.Is(tt.err, tt.wantCause) {
				t.Errorf("errors.Is(%v, %v) = false; cancel predicates would treat a clean abort as a failure", tt.err, tt.wantCause)
			}
			if errors.Is(tt.err, tt.notCause) {
				t.Errorf("errors.Is(%v, %v) = true; Esc and Ctrl+C must stay distinguishable", tt.err, tt.notCause)
			}
		})
	}
}

// The bare sentinel is an errors.Is target only. If it ever gains a cause it
// would make both abort kinds indistinguishable through the umbrella.
func TestErrCancelled_IsBareTarget(t *testing.T) {
	if errors.Unwrap(ErrCancelled) != nil {
		t.Error("ErrCancelled wraps something; it must stay a bare matching target")
	}
}
