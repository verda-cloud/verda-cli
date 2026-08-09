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

package tui

import (
	"context"
	"errors"
	"testing"
)

// The Resolve*Config defaults are UX contracts: every prompt in the CLI
// inherits them, so a silent change here alters paging and wrap-around
// behavior everywhere at once with nothing to catch it.
func TestResolveConfigs_Defaults(t *testing.T) {
	t.Run("select paginates at 10 and wraps", func(t *testing.T) {
		cfg := ResolveSelectConfig(nil)
		if cfg.PageSize != 10 {
			t.Errorf("PageSize = %d, want 10", cfg.PageSize)
		}
		if !cfg.Loop {
			t.Error("Loop = false, want true")
		}
		if cfg.ShowHints {
			t.Error("ShowHints defaulted to true; the hint bar must stay opt-in per call site")
		}
	})

	t.Run("multiselect matches select", func(t *testing.T) {
		cfg := ResolveMultiSelectConfig(nil)
		if cfg.PageSize != 10 || !cfg.Loop {
			t.Errorf("PageSize/Loop = %d/%v, want 10/true", cfg.PageSize, cfg.Loop)
		}
	})

	t.Run("livelist matches select", func(t *testing.T) {
		cfg := ResolveLiveListConfig(nil)
		if cfg.PageSize != 10 || !cfg.Loop {
			t.Errorf("PageSize/Loop = %d/%v, want 10/true", cfg.PageSize, cfg.Loop)
		}
	})

	t.Run("editor defaults to .txt with help", func(t *testing.T) {
		cfg := ResolveEditorConfig(nil)
		if cfg.FileExt != ".txt" {
			t.Errorf("FileExt = %q, want .txt", cfg.FileExt)
		}
		if !cfg.ShowHelp {
			t.Error("ShowHelp = false, want true")
		}
	})

	t.Run("confirm defaults to no", func(t *testing.T) {
		if ResolveConfirmConfig(nil).Default {
			t.Error("Default = true; a confirm must never pre-answer yes")
		}
	})
}

// Options are applied in slice order, so a later option wins. Call sites rely
// on this to layer a caller override on top of a shared base option set.
func TestResolveSelectConfig_LastOptionWins(t *testing.T) {
	cfg := ResolveSelectConfig([]SelectOption{
		WithPageSize(5),
		WithPageSize(20),
	})
	if cfg.PageSize != 20 {
		t.Errorf("PageSize = %d, want 20 (later option must win)", cfg.PageSize)
	}
}

func TestWithShowHints_TogglesHintBar(t *testing.T) {
	if !ResolveSelectConfig([]SelectOption{WithShowHints(true)}).ShowHints {
		t.Error("WithShowHints(true) did not set ShowHints")
	}
	if !ResolveMultiSelectConfig([]MultiSelectOption{WithMultiSelectShowHints(true)}).ShowHints {
		t.Error("WithMultiSelectShowHints(true) did not set ShowHints")
	}
}

// Relabel lazily allocates its map; the nil-map path is the common one since
// configs start zero-valued.
func TestRelabel_AllocatesAndAccumulates(t *testing.T) {
	cfg := ResolveSelectConfig([]SelectOption{
		WithSelectRelabel("up-down", "move"),
		WithSelectRelabel("enter", "choose"),
		WithSelectRelabel("up-down", "navigate"),
	})
	if got := len(cfg.RelabelByID); got != 2 {
		t.Fatalf("len(RelabelByID) = %d, want 2", got)
	}
	if got := cfg.RelabelByID["up-down"]; got != "navigate" {
		t.Errorf("RelabelByID[up-down] = %q, want navigate (later relabel wins)", got)
	}
	if got := cfg.RelabelByID["enter"]; got != "choose" {
		t.Errorf("RelabelByID[enter] = %q, want choose", got)
	}
}

func TestHide_AccumulatesAcrossCalls(t *testing.T) {
	cfg := ResolveSelectConfig([]SelectOption{
		WithSelectHide("filter"),
		WithSelectHide("esc", "ctrl+c"),
	})
	want := []string{"filter", "esc", "ctrl+c"}
	if len(cfg.HiddenByID) != len(want) {
		t.Fatalf("HiddenByID = %v, want %v", cfg.HiddenByID, want)
	}
	for i, id := range want {
		if cfg.HiddenByID[i] != id {
			t.Errorf("HiddenByID[%d] = %q, want %q", i, cfg.HiddenByID[i], id)
		}
	}
}

// The CLI maps cancel to a clean exit and everything else to a failure. If
// ErrNoTerminal were ever confused with a cancel sentinel, a piped/redirected
// invocation would exit 0 having silently done nothing.
func TestErrorSentinels_StayDistinct(t *testing.T) {
	if errors.Is(ErrNoTerminal, ErrInterrupted) {
		t.Error("ErrNoTerminal matches ErrInterrupted; a non-TTY failure would look like a user cancel")
	}
	if errors.Is(ErrNoTerminal, context.Canceled) {
		t.Error("ErrNoTerminal matches context.Canceled; a non-TTY failure would look like Esc")
	}
	if errors.Is(ErrInterrupted, context.Canceled) {
		t.Error("ErrInterrupted matches context.Canceled; Ctrl+C and Esc must stay distinguishable")
	}
}

// nil fields are the documented handoff to the backend ("nil means os.Stdin at
// runtime"), despite the function name suggesting concrete streams. Pinned so a
// well-meaning change to real os.* handles doesn't silently bypass the
// IOStreams a command passed in.
func TestDefaultIO_LeavesStreamsNilForBackend(t *testing.T) {
	io := DefaultIO()
	if io.In != nil || io.Out != nil || io.ErrOut != nil {
		t.Errorf("DefaultIO() = %+v, want all-nil so the backend supplies streams", io)
	}
}

func TestDefault_PanicsWithoutRegisteredBackend(t *testing.T) {
	mu.Lock()
	saved := builder
	builder = nil
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		builder = saved
		mu.Unlock()
	})

	defer func() {
		if recover() == nil {
			t.Error("Default() did not panic with no backend registered")
		}
	}()
	_ = Default()
}

func TestRegisterBuilder_DefaultUsesRegisteredFactory(t *testing.T) {
	mu.Lock()
	saved := builder
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		builder = saved
		mu.Unlock()
	})

	sentinel := &stubPrompter{}
	RegisterBuilder(func(_ ...func(*IO)) Prompter { return sentinel })

	if got := Default(); got != sentinel {
		t.Errorf("Default() = %v, want the registered builder's Prompter", got)
	}
}

type stubPrompter struct{ Prompter }
