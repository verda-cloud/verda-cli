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

package bubbletea

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

func runtimeNumGoroutine() int { return runtime.NumGoroutine() }

// --- helpers ---

func makeLiveRows(keys ...string) []tui.LiveRow {
	out := make([]tui.LiveRow, len(keys))
	for i, k := range keys {
		out[i] = tui.LiveRow{Key: k, Label: k + " ..."}
	}
	return out
}

func newLL(rows []tui.LiveRow, opts ...tui.LiveListOption) liveListModel {
	cfg := tui.ResolveLiveListConfig(opts)
	return newLiveListModel("Pick", rows, cfg)
}

func llKey(m liveListModel, msg tea.KeyPressMsg) liveListModel {
	updated, _ := m.Update(msg)
	return updated.(liveListModel)
}

func llRune(m liveListModel, r rune) liveListModel {
	return llKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

func llSendUpdate(m liveListModel, key, label string, err error) liveListModel {
	updated, _ := m.Update(liveListUpdateMsg{Key: key, Label: label, Err: err})
	return updated.(liveListModel)
}

// --- Spec table tests ---

// "No updates received" — behaves identically to Select.
func TestLiveList_NoUpdates_BehavesLikeSelect(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta", "gamma"))
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chosen {
		t.Fatal("Enter should choose")
	}
	if got, _ := m.Result(); got != 1 {
		t.Errorf("Result() = %v, want 1 (beta)", got)
	}
}

// "Update before user input" — row label changes in View().
func TestLiveList_UpdateChangesViewLabel(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	m = llSendUpdate(m, "alpha", "alpha (running)", nil)
	view := m.View().Content
	if !strings.Contains(view, "alpha (running)") {
		t.Errorf("expected updated label in view, got:\n%s", view)
	}
	if strings.Contains(view, "alpha ...") {
		t.Errorf("placeholder should be replaced, got:\n%s", view)
	}
}

// "Update changes filter membership" — row drops in/out on refilter.
func TestLiveList_UpdateChangesFilterMembership(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	// Type 'z' (not vim-nav, doesn't appear in placeholders) → empty match.
	m = llRune(m, 'z')
	if len(m.matched) != 0 {
		t.Fatalf("setup: expected 0 matches for filter 'z', got %d", len(m.matched))
	}
	// Update alpha to contain 'z'.
	m = llSendUpdate(m, "alpha", "alpha (zzz)", nil)
	if len(m.matched) != 1 || m.keys[m.matched[0]] != "alpha" {
		t.Errorf("alpha should now match filter, got matched=%v", m.matched)
	}
}

// "Update with unknown Key" — silently dropped.
func TestLiveList_UnknownKeyDropped(t *testing.T) {
	m := newLL(makeLiveRows("alpha"))
	before := m.View().Content
	m = llSendUpdate(m, "nonexistent", "ignored", nil)
	after := m.View().Content
	if before != after {
		t.Errorf("unknown key should not change view\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// "Update with Err set" — row renders with error style; Label visible.
func TestLiveList_ErrorStyleApplied(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	m = llSendUpdate(m, "beta", "beta: rate limited", errors.New("429"))

	idx := m.keyIndex["beta"]
	if !m.errs[idx] {
		t.Fatal("expected beta marked as errored")
	}
	view := m.View().Content
	if !strings.Contains(view, "beta: rate limited") {
		t.Errorf("error label should still be visible, got:\n%s", view)
	}
}

// A successful follow-up update clears the error flag.
func TestLiveList_ErrorClearedByLaterSuccess(t *testing.T) {
	m := newLL(makeLiveRows("alpha"))
	m = llSendUpdate(m, "alpha", "alpha: err", errors.New("boom"))
	idx := m.keyIndex["alpha"]
	if !m.errs[idx] {
		t.Fatal("setup: expected errored")
	}
	m = llSendUpdate(m, "alpha", "alpha (ok)", nil)
	if m.errs[idx] {
		t.Error("successful update should clear error")
	}
}

// "Multiple updates same Key" — last write wins.
func TestLiveList_LastWriteWins(t *testing.T) {
	m := newLL(makeLiveRows("alpha"))
	m = llSendUpdate(m, "alpha", "first", nil)
	m = llSendUpdate(m, "alpha", "second", nil)
	m = llSendUpdate(m, "alpha", "third", nil)
	if got := m.choices[0]; got != "third" {
		t.Errorf("expected 'third', got %q", got)
	}
}

// "Cursor preservation across update" — cursor stays on hovered key
// even when its filtered index shifts.
func TestLiveList_CursorPreservedByKey(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta", "gamma"))
	// Move cursor to beta.
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.keys[m.matched[m.cursor]] != "beta" {
		t.Fatalf("setup: cursor should be on beta, got %q", m.keys[m.matched[m.cursor]])
	}

	// Now update alpha's label such that it sorts/filters differently.
	// (Refilter resets cursor to 0; preservation logic must restore.)
	m = llSendUpdate(m, "alpha", "alpha (changed)", nil)

	if got := m.keys[m.matched[m.cursor]]; got != "beta" {
		t.Errorf("cursor should remain on beta after unrelated update, got %q", got)
	}
}

// Cursor preservation also works when the update is for the row
// currently under the cursor.
func TestLiveList_CursorStaysOnSelfUpdate(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyDown}) // hover beta
	m = llSendUpdate(m, "beta", "beta (running)", nil)
	if got := m.keys[m.matched[m.cursor]]; got != "beta" {
		t.Errorf("cursor should stay on beta, got %q", got)
	}
}

// "Selection works mid-update" — Enter still returns the cursor row.
func TestLiveList_SelectMidUpdate(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta", "gamma"))
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyDown}) // beta
	m = llSendUpdate(m, "alpha", "alpha (updated)", nil)
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chosen {
		t.Fatal("Enter should choose")
	}
	idx, _ := m.Result()
	if idx != 1 {
		t.Errorf("Result() = %v, want 1 (beta)", idx)
	}
}

// "Ctrl+C / Esc" — same cancel semantics as Select.
func TestLiveList_EscAndCtrlC(t *testing.T) {
	// Esc.
	m := newLL(makeLiveRows("a"))
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.aborted {
		t.Error("Esc should set aborted")
	}
	// Feeding back the GoBackMsg should quit.
	_, cmd := m.Update(GoBackMsg{})
	if cmd == nil {
		t.Fatal("GoBackMsg should produce a quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}

	// Ctrl+C.
	m = newLL(makeLiveRows("a"))
	m = llKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.interrupted {
		t.Error("ctrl+c should set interrupted")
	}
}

// "WithShowHints(true)" — hint bar renders below choices.
func TestLiveList_ShowHintsRendersBar(t *testing.T) {
	m := newLL(makeLiveRows("alpha"), tui.WithLiveListShowHints(true))
	view := m.View().Content
	if !strings.Contains(view, "↑/↓ navigate") {
		t.Errorf("expected default hint bar, got:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+c exit") {
		t.Errorf("expected ctrl+c exit in hints, got:\n%s", view)
	}
}

// "WithLiveListRelabel" — hint bar shows the relabeled string.
func TestLiveList_RelabelAppearsInHints(t *testing.T) {
	m := newLL(makeLiveRows("alpha"),
		tui.WithLiveListShowHints(true),
		tui.WithLiveListRelabel("esc", "esc abort"),
	)
	view := m.View().Content
	if !strings.Contains(view, "esc abort") {
		t.Errorf("expected relabeled esc, got:\n%s", view)
	}
	if strings.Contains(view, "esc back") {
		t.Errorf("default esc label should be replaced, got:\n%s", view)
	}
}

// "WithLiveListAddBindings" — custom binding dispatches; appears in
// hint bar.
func TestLiveList_AddBindings(t *testing.T) {
	fired := false
	refresh := KeyBinding[liveListModel]{
		ID:    "refresh",
		Match: MatchRune('r', tea.ModCtrl),
		Label: func(*liveListModel) string { return "ctrl+r refresh" },
		Handle: func(_ *liveListModel, _ tea.KeyPressMsg) (tea.Cmd, bool) {
			fired = true
			return nil, true
		},
	}
	m := newLL(makeLiveRows("alpha"),
		tui.WithLiveListShowHints(true),
		WithLiveListAddBindings(refresh),
	)
	view := m.View().Content
	if !strings.Contains(view, "ctrl+r refresh") {
		t.Errorf("custom binding label missing, got:\n%s", view)
	}
	_ = llKey(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !fired {
		t.Error("custom binding did not fire")
	}
}

// Hide path: ID hidden suppresses the label, key handling preserved.
func TestLiveList_HideSuppressesLabel(t *testing.T) {
	m := newLL(makeLiveRows("alpha"),
		tui.WithLiveListShowHints(true),
		tui.WithLiveListHide("exit"),
	)
	view := m.View().Content
	if strings.Contains(view, "ctrl+c exit") {
		t.Errorf("exit hint should be hidden, got:\n%s", view)
	}
	m = llKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.interrupted {
		t.Error("ctrl+c should still trigger interrupt after hide")
	}
}

// Default hint sequence matches Select's — locks compat with the
// existing select hint string consumers may assert against.
func TestLiveList_DefaultHintsOrder(t *testing.T) {
	m := newLL(makeLiveRows("alpha"))
	got := strings.Join(m.Hints(), " · ")
	want := "↑/↓ navigate · type to filter · enter select · esc back · ctrl+c exit"
	if got != want {
		t.Errorf("hint sequence drifted\n got: %s\nwant: %s", got, want)
	}
}

// A late update that arrives after Enter (chosen) must not panic in
// View, even when it would narrow the active filter to zero matches.
// bubbletea drains queued msgs after tea.Quit, so a pumped update can
// land between Enter and teardown.
func TestLiveList_NoPanicOnUpdateAfterChosen(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	// Filter down to just the alpha row.
	for _, r := range "alpha" {
		m = llRune(m, r)
	}
	if len(m.matched) != 1 {
		t.Fatalf("setup: expected 1 match for filter 'alpha', got %d", len(m.matched))
	}
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chosen {
		t.Fatal("setup: Enter should choose")
	}
	// Late update renames alpha so the active filter "alpha" no longer
	// matches it → refilter would empty matched.
	m = llSendUpdate(m, "alpha", "zzz", nil)
	// Must not panic.
	view := m.View().Content
	if view == "" {
		t.Fatal("expected non-empty view after chosen")
	}
}

// --- Prompter-level (full integration) tests ---

// "Channel closed before selection" — picker still selectable until
// user acts. Uses Prompter.LiveList with a manually-driven program
// would be heavyweight; we test the equivalent at the model level:
// after the update stream is exhausted, Update on KeyEnter still
// completes the prompt.
func TestLiveList_ChannelClosedStillSelectable(t *testing.T) {
	m := newLL(makeLiveRows("alpha", "beta"))
	// Simulate a stream that delivered then would close.
	m = llSendUpdate(m, "alpha", "alpha (running)", nil)
	m = llSendUpdate(m, "beta", "beta (running)", nil)
	m = llKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chosen {
		t.Error("should still be selectable after stream exhausted")
	}
}

// Pump exits via the done channel when the bubbletea program returns,
// even if ctx stays alive and the caller never closes updates. This is
// the leak-prevention guarantee — exercised on the helper directly so
// it doesn't depend on driving a real tea.Program.
func TestLiveList_PumpExitsOnDoneClose(t *testing.T) {
	updates := make(chan tui.LiveListUpdate) // never closed
	done := make(chan struct{})
	exited := make(chan struct{})

	go func() {
		pumpLiveListUpdates(context.Background(), done, updates, func(liveListUpdateMsg) {})
		close(exited)
	}()

	close(done)

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not exit after done close")
	}
}

// Pump exits via ctx.Done when ctx is canceled with the program still
// running and updates still open.
func TestLiveList_PumpExitsOnCtxDone(t *testing.T) {
	updates := make(chan tui.LiveListUpdate)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan struct{})

	go func() {
		pumpLiveListUpdates(ctx, done, updates, func(liveListUpdateMsg) {})
		close(exited)
	}()

	cancel()

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not exit after ctx cancel")
	}
}

// Pump exits when the updates channel is closed.
func TestLiveList_PumpExitsOnUpdatesClose(t *testing.T) {
	updates := make(chan tui.LiveListUpdate)
	done := make(chan struct{})
	exited := make(chan struct{})

	go func() {
		pumpLiveListUpdates(context.Background(), done, updates, func(liveListUpdateMsg) {})
		close(exited)
	}()

	close(updates)

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("pump did not exit after updates close")
	}
}

// Updates flowing through the pump reach the supplied send fn.
func TestLiveList_PumpForwardsUpdates(t *testing.T) {
	updates := make(chan tui.LiveListUpdate, 2)
	done := make(chan struct{})
	defer close(done)

	got := make(chan liveListUpdateMsg, 2)
	go pumpLiveListUpdates(context.Background(), done, updates, func(m liveListUpdateMsg) {
		got <- m
	})

	updates <- tui.LiveListUpdate{Key: "k1", Label: "L1"}
	updates <- tui.LiveListUpdate{Key: "k2", Label: "L2", Err: errors.New("e")}

	for i, want := range []liveListUpdateMsg{
		{Key: "k1", Label: "L1"},
		{Key: "k2", Label: "L2", Err: errors.New("e")},
	} {
		select {
		case m := <-got:
			if m.Key != want.Key || m.Label != want.Label {
				t.Errorf("update %d = %+v, want %+v", i, m, want)
			}
			if (m.Err == nil) != (want.Err == nil) {
				t.Errorf("update %d err presence mismatch", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("update %d not forwarded", i)
		}
	}
}

// Full integration check: LiveList returns when ctx is canceled, and
// the call doesn't leak goroutines. Does not strictly prove the
// done-close branch (impossible without a TTY to drive Enter) — the
// branch-specific guarantees are covered by TestLiveList_PumpExitsOnDoneClose.
func TestLiveList_ReturnsAfterCtxCancel(t *testing.T) {
	p := New()
	rows := makeLiveRows("alpha", "beta")
	updates := make(chan tui.LiveListUpdate) // never written, never closed

	pre := runtimeNumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = p.LiveList(ctx, "Pick", rows, updates)
	})
	time.Sleep(50 * time.Millisecond)
	cancel()

	returned := make(chan struct{})
	go func() { wg.Wait(); close(returned) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("LiveList did not return after ctx cancel")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtimeNumGoroutine() <= pre+1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("goroutine leak: pre=%d post=%d", pre, runtimeNumGoroutine())
}
