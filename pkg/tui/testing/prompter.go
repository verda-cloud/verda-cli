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

// Package testing provides a programmable tui.Prompter for use in tests.
// Responses are queued and returned in order.
package testing

import (
	"context"
	"fmt"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// Prompter is a test double that returns pre-configured responses.
type Prompter struct {
	confirms     []bool
	textInputs   []string
	passwords    []string
	selects      []int
	multiSelects [][]int
	liveLists    []int
	editors      []string
}

// New creates a test Prompter.
func New() *Prompter { return &Prompter{} }

// AddConfirm queues a confirm response.
func (p *Prompter) AddConfirm(v bool) *Prompter {
	p.confirms = append(p.confirms, v)
	return p
}

// AddTextInput queues a text input response.
func (p *Prompter) AddTextInput(v string) *Prompter {
	p.textInputs = append(p.textInputs, v)
	return p
}

// AddPassword queues a password response.
func (p *Prompter) AddPassword(v string) *Prompter {
	p.passwords = append(p.passwords, v)
	return p
}

// AddSelect queues a select response (index).
func (p *Prompter) AddSelect(v int) *Prompter {
	p.selects = append(p.selects, v)
	return p
}

// AddMultiSelect queues a multi-select response (indices).
func (p *Prompter) AddMultiSelect(v []int) *Prompter {
	p.multiSelects = append(p.multiSelects, v)
	return p
}

// AddLiveList queues a live-list response (index into rows).
func (p *Prompter) AddLiveList(v int) *Prompter {
	p.liveLists = append(p.liveLists, v)
	return p
}

// AddEditor queues an editor response.
func (p *Prompter) AddEditor(v string) *Prompter {
	p.editors = append(p.editors, v)
	return p
}

func (p *Prompter) Confirm(_ context.Context, _ string, _ ...tui.ConfirmOption) (bool, error) {
	if len(p.confirms) == 0 {
		return false, fmt.Errorf("testing.Prompter: no confirm responses queued")
	}
	v := p.confirms[0]
	p.confirms = p.confirms[1:]
	return v, nil
}

func (p *Prompter) TextInput(_ context.Context, _ string, _ ...tui.TextInputOption) (string, error) {
	if len(p.textInputs) == 0 {
		return "", fmt.Errorf("testing.Prompter: no text input responses queued")
	}
	v := p.textInputs[0]
	p.textInputs = p.textInputs[1:]
	return v, nil
}

func (p *Prompter) Password(_ context.Context, _ string) (string, error) {
	if len(p.passwords) == 0 {
		return "", fmt.Errorf("testing.Prompter: no password responses queued")
	}
	v := p.passwords[0]
	p.passwords = p.passwords[1:]
	return v, nil
}

func (p *Prompter) Select(_ context.Context, _ string, _ []string, _ ...tui.SelectOption) (int, error) {
	if len(p.selects) == 0 {
		return -1, fmt.Errorf("testing.Prompter: no select responses queued")
	}
	v := p.selects[0]
	p.selects = p.selects[1:]
	return v, nil
}

func (p *Prompter) MultiSelect(_ context.Context, _ string, _ []string, _ ...tui.MultiSelectOption) ([]int, error) {
	if len(p.multiSelects) == 0 {
		return nil, fmt.Errorf("testing.Prompter: no multi-select responses queued")
	}
	v := p.multiSelects[0]
	p.multiSelects = p.multiSelects[1:]
	return v, nil
}

// LiveList returns the next queued index. The updates channel (if
// non-nil) is drained in the background so producers don't block;
// drained values are discarded. Tests asserting on update behavior
// should drive the model directly instead of going through this fake.
//
// Lifecycle contract: because the fake returns immediately, the drain
// goroutine outlives the call. Callers passing a non-nil updates channel
// MUST either cancel ctx or close updates so the goroutine can exit;
// otherwise it leaks for the lifetime of the test binary.
func (p *Prompter) LiveList(ctx context.Context, _ string, _ []tui.LiveRow, updates <-chan tui.LiveListUpdate, _ ...tui.LiveListOption) (int, error) {
	if updates != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case _, ok := <-updates:
					if !ok {
						return
					}
				}
			}
		}()
	}
	if len(p.liveLists) == 0 {
		return -1, fmt.Errorf("testing.Prompter: no live-list responses queued")
	}
	v := p.liveLists[0]
	p.liveLists = p.liveLists[1:]
	return v, nil
}

func (p *Prompter) Editor(_ context.Context, _ string, _ ...tui.EditorOption) (string, error) {
	if len(p.editors) == 0 {
		return "", fmt.Errorf("testing.Prompter: no editor responses queued")
	}
	v := p.editors[0]
	p.editors = p.editors[1:]
	return v, nil
}

// --- Status test doubles ---

// SpinnerHandle is a no-op handle for testing.
type SpinnerHandle struct {
	Messages     []string
	FinalMessage string
	Stopped      bool
}

func (h *SpinnerHandle) UpdateMessage(msg string) {
	h.Messages = append(h.Messages, msg)
}

func (h *SpinnerHandle) Stop(finalMessage string) {
	h.FinalMessage = finalMessage
	h.Stopped = true
}

// Interrupted always reports false: the test double never starts a program,
// so there is nothing a user could have Ctrl+C'd.
func (h *SpinnerHandle) Interrupted() bool { return false }

// ProgressHandle is a no-op handle for testing.
type ProgressHandle struct {
	Percent      float64
	FinalMessage string
	Stopped      bool
}

func (h *ProgressHandle) SetPercent(p float64) {
	h.Percent = p
}

func (h *ProgressHandle) Increment(delta float64) {
	h.Percent += delta
	if h.Percent > 1.0 {
		h.Percent = 1.0
	}
}

func (h *ProgressHandle) Stop(finalMessage string) {
	h.FinalMessage = finalMessage
	h.Stopped = true
}

// Interrupted always reports false: the test double never starts a program.
func (h *ProgressHandle) Interrupted() bool { return false }

func (p *Prompter) Spinner(_ context.Context, _ string, _ ...tui.SpinnerOption) (tui.SpinnerHandle, error) {
	return &SpinnerHandle{}, nil
}

func (p *Prompter) Progress(_ context.Context, _ string, _ ...tui.ProgressOption) (tui.ProgressHandle, error) {
	return &ProgressHandle{}, nil
}

func (p *Prompter) Table(_ context.Context, _ []string, _ [][]string, _ ...tui.TableOption) error {
	return nil
}

func (p *Prompter) Pager(_ context.Context, _ string, _ ...tui.PagerOption) error {
	return nil
}

// Compile-time interface checks.
var _ tui.Prompter = (*Prompter)(nil)
var _ tui.Status = (*Prompter)(nil)
var _ tui.LiveLister = (*Prompter)(nil)
