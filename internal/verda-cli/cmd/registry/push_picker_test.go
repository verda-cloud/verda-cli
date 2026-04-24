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

package registry

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// pickerFixedClock returns a now() function fixed to t.
func pickerFixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// pickerAt is a convenience constructor for DaemonImage entries in tests —
// one image with the given tags, size zero, and Created = baseTime + offset.
func pickerAt(offsetSeconds int64, tags ...string) DaemonImage {
	return DaemonImage{
		ID:       "sha256:fake",
		RepoTags: tags,
		Size:     0,
		Created:  time.Unix(1_700_000_000+offsetSeconds, 0),
	}
}

// keyRune builds a plain-rune KeyPressMsg (no modifiers). The v2 API keeps
// the rune in Code; Text is where tea's input pipeline writes the printable
// form — we set both so filter mode picks up the character like the real
// input would.
func keyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// keySpecial builds a KeyPressMsg for a non-printable named key (Enter,
// Esc, Backspace, arrows, ...). Text is empty for special keys — the
// upstream key package documents this invariant.
func keySpecial(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

// TestPicker_Expansion — dangling images are skipped, no-tag images are
// skipped, and multi-tag images expand to one row per tag.
func TestPicker_Expansion(t *testing.T) {
	t.Parallel()
	imgs := []DaemonImage{
		// multi-tag: 2 rows
		pickerAt(100, "my-app:v1", "my-app:latest"),
		// dangling sentinel: skipped
		pickerAt(90, "<none>:<none>"),
		// no tags at all: skipped
		pickerAt(80),
		// mixed: one good tag, one sentinel — only the good tag survives
		pickerAt(70, "nginx:latest", "<none>:<none>"),
	}
	m := newPushPickerModel(imgs)
	if len(m.items) != 3 {
		t.Fatalf("len(items) = %d, want 3 (my-app:v1, my-app:latest, nginx:latest): %+v",
			len(m.items), m.items)
	}
	refs := make([]string, len(m.items))
	for i, it := range m.items {
		refs[i] = it.Ref
	}
	for _, want := range []string{"my-app:v1", "my-app:latest", "nginx:latest"} {
		found := false
		for _, got := range refs {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing ref %q in picker rows: %v", want, refs)
		}
	}
}

// TestPicker_SortedByCreatedDesc — rows are in newest-first order.
func TestPicker_SortedByCreatedDesc(t *testing.T) {
	t.Parallel()
	imgs := []DaemonImage{
		pickerAt(10, "old:1"),
		pickerAt(30, "newest:3"),
		pickerAt(20, "mid:2"),
	}
	m := newPushPickerModel(imgs)
	got := []string{m.items[0].Ref, m.items[1].Ref, m.items[2].Ref}
	want := []string{"newest:3", "mid:2", "old:1"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("items[%d].Ref = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
}

// TestPicker_InitialStateNothingSelected — Selected() is empty on a fresh
// model.
func TestPicker_InitialStateNothingSelected(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(10, "a:1"),
		pickerAt(20, "b:2"),
	})
	if got := m.Selected(); len(got) != 0 {
		t.Errorf("Selected() = %+v, want empty", got)
	}
	if m.Canceled() {
		t.Error("fresh model reports Canceled()=true")
	}
}

// TestPicker_SpaceTogglesSelection — pressing space flips the item under
// the cursor; pressing it again clears the selection.
func TestPicker_SpaceTogglesSelection(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(10, "a:1"),
		pickerAt(20, "b:2"),
	})
	// Cursor starts at 0 ("b:2" since sorted newest-first -> "b:2" at 0).
	_, _ = m.Update(keySpecial(tea.KeySpace))
	sel := m.Selected()
	if len(sel) != 1 {
		t.Fatalf("after 1 space press: Selected() = %d items, want 1", len(sel))
	}
	if sel[0].Ref != m.items[0].Ref {
		t.Errorf("selected ref = %q, want %q", sel[0].Ref, m.items[0].Ref)
	}
	_, _ = m.Update(keySpecial(tea.KeySpace))
	if got := m.Selected(); len(got) != 0 {
		t.Errorf("after 2nd space press: Selected() = %+v, want empty", got)
	}
}

// TestPicker_CursorUpDown — j/k move cursor, clamped at ends.
func TestPicker_CursorUpDown(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "a:1"),
		pickerAt(20, "b:2"),
		pickerAt(10, "c:3"),
	})
	if m.cursor != 0 {
		t.Fatalf("cursor start = %d, want 0", m.cursor)
	}
	_, _ = m.Update(keyRune('j'))
	if m.cursor != 1 {
		t.Errorf("after j: cursor = %d, want 1", m.cursor)
	}
	_, _ = m.Update(keyRune('j'))
	if m.cursor != 2 {
		t.Errorf("after jj: cursor = %d, want 2", m.cursor)
	}
	// Clamp at bottom.
	_, _ = m.Update(keyRune('j'))
	if m.cursor != 2 {
		t.Errorf("after jjj: cursor = %d, want 2 (clamped)", m.cursor)
	}
	_, _ = m.Update(keyRune('k'))
	if m.cursor != 1 {
		t.Errorf("after k: cursor = %d, want 1", m.cursor)
	}
	_, _ = m.Update(keyRune('k'))
	_, _ = m.Update(keyRune('k'))
	if m.cursor != 0 {
		t.Errorf("after kkk: cursor = %d, want 0 (clamped)", m.cursor)
	}
}

// TestPicker_EnterSubmits — Enter sets submitted and returns tea.Quit.
func TestPicker_EnterSubmits(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{pickerAt(10, "a:1")})
	_, cmd := m.Update(keySpecial(tea.KeyEnter))
	if !m.submitted {
		t.Error("submitted = false after Enter")
	}
	if m.canceled {
		t.Error("canceled = true after Enter (should be false)")
	}
	if !cmdIsQuit(cmd) {
		t.Error("Enter did not return tea.Quit")
	}
}

// TestPicker_EscCancels — Esc cancels and returns tea.Quit.
func TestPicker_EscCancels(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{pickerAt(10, "a:1")})
	_, cmd := m.Update(keySpecial(tea.KeyEsc))
	if !m.canceled {
		t.Error("canceled = false after Esc")
	}
	if m.submitted {
		t.Error("submitted = true after Esc (should be false)")
	}
	if !cmdIsQuit(cmd) {
		t.Error("Esc did not return tea.Quit")
	}
	if got := m.Selected(); len(got) != 0 {
		t.Errorf("Selected() on canceled model = %+v, want empty", got)
	}
}

// TestPicker_CtrlCCancels — Ctrl+C cancels and returns tea.Quit.
func TestPicker_CtrlCCancels(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{pickerAt(10, "a:1")})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.canceled {
		t.Error("canceled = false after Ctrl+C")
	}
	if !cmdIsQuit(cmd) {
		t.Error("Ctrl+C did not return tea.Quit")
	}
}

// TestPicker_ToggleAllSelectsAll — 'a' with nothing selected selects all
// visible rows.
func TestPicker_ToggleAllSelectsAll(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "a:1"),
		pickerAt(20, "b:2"),
		pickerAt(10, "c:3"),
	})
	_, _ = m.Update(keyRune('a'))
	if got := len(m.Selected()); got != 3 {
		t.Errorf("after 'a' on empty selection: Selected() count = %d, want 3", got)
	}
}

// TestPicker_ToggleAllDeselectsWhenAllSelected — 'a' with all selected
// clears the selection.
func TestPicker_ToggleAllDeselectsWhenAllSelected(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "a:1"),
		pickerAt(20, "b:2"),
		pickerAt(10, "c:3"),
	})
	_, _ = m.Update(keyRune('a')) // select all
	_, _ = m.Update(keyRune('a')) // deselect all
	if got := len(m.Selected()); got != 0 {
		t.Errorf("after 'a' x2: Selected() count = %d, want 0", got)
	}
}

// TestPicker_InvertVisible — 'A' inverts the selection state of every
// visible row.
func TestPicker_InvertVisible(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "a:1"),
		pickerAt(20, "b:2"),
		pickerAt(10, "c:3"),
	})
	// Select row 1 ("b:2").
	_, _ = m.Update(keyRune('j'))
	_, _ = m.Update(keySpecial(tea.KeySpace))
	if got := len(m.Selected()); got != 1 {
		t.Fatalf("pre-invert: got %d selected, want 1", got)
	}
	_, _ = m.Update(keyRune('A'))
	if got := len(m.Selected()); got != 2 {
		t.Errorf("after 'A': Selected() count = %d, want 2", got)
	}
}

// TestPicker_FilterNarrowsVisibleRows — pressing `/` enters filter mode;
// typed characters narrow visible rows and View() renders only matches.
func TestPicker_FilterNarrowsVisibleRows(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "nginx:1"),
		pickerAt(20, "nginx:2"),
		pickerAt(10, "my-app:1"),
	})
	_, _ = m.Update(keyRune('/'))
	if !m.filtering {
		t.Fatal("'/' did not enter filter mode")
	}
	// Type "ngi"
	for _, r := range "ngi" {
		_, _ = m.Update(keyRune(r))
	}
	vis := m.visibleIndexes()
	if len(vis) != 2 {
		t.Errorf("visible after filter='ngi': %d rows, want 2 (%v)", len(vis), vis)
	}

	// View output should mention nginx:* but not my-app:*.
	out := m.View().Content
	if !strings.Contains(out, "nginx:1") || !strings.Contains(out, "nginx:2") {
		t.Errorf("view missing nginx refs under filter:\n%s", out)
	}
	if strings.Contains(out, "my-app:1") {
		t.Errorf("view contains filtered-out ref 'my-app:1':\n%s", out)
	}

	// Commit filter with Enter. Cursor should move to first visible.
	_, _ = m.Update(keySpecial(tea.KeyEnter))
	if m.filtering {
		t.Error("still filtering after Enter")
	}
	if !m.isVisible(m.cursor) {
		t.Errorf("after Enter: cursor=%d not on a visible row (filter=%q)", m.cursor, m.filter)
	}

	// Toggling selection should only hit visible rows. Select both visible
	// nginx rows via 'a'.
	_, _ = m.Update(keyRune('a'))
	sel := m.Selected()
	if len(sel) != 2 {
		t.Errorf("after 'a' under filter: Selected() = %d, want 2", len(sel))
	}
	for _, it := range sel {
		if !strings.HasPrefix(it.Ref, "nginx:") {
			t.Errorf("Selected includes non-visible ref %q", it.Ref)
		}
	}
}

// TestPicker_FilterEscClearsFilter — pressing Esc while filtering clears
// the filter and returns to normal mode without canceling the picker.
func TestPicker_FilterEscClearsFilter(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "nginx:1"),
		pickerAt(10, "my-app:1"),
	})
	_, _ = m.Update(keyRune('/'))
	for _, r := range "ngi" {
		_, _ = m.Update(keyRune(r))
	}
	_, cmd := m.Update(keySpecial(tea.KeyEsc))
	if cmdIsQuit(cmd) {
		t.Error("Esc while filtering returned tea.Quit; should only clear filter")
	}
	if m.filtering {
		t.Error("filtering = true after Esc")
	}
	if m.filter != "" {
		t.Errorf("filter = %q, want empty", m.filter)
	}
	if m.canceled {
		t.Error("Esc while filtering set canceled; picker must stay open")
	}
	if got := len(m.visibleIndexes()); got != 2 {
		t.Errorf("visible after filter cleared: %d, want 2", got)
	}
}

// TestPicker_SelectedReturnsCanonicalOrder — Selected() follows the
// display order (Created desc), not the order the user ticked rows.
func TestPicker_SelectedReturnsCanonicalOrder(t *testing.T) {
	t.Parallel()
	m := newPushPickerModel([]DaemonImage{
		pickerAt(30, "a:1"),
		pickerAt(20, "b:2"),
		pickerAt(10, "c:3"),
	})
	// Select c:3 first (bottom), then a:1 (top).
	_, _ = m.Update(keyRune('j'))             // -> b:2
	_, _ = m.Update(keyRune('j'))             // -> c:3
	_, _ = m.Update(keySpecial(tea.KeySpace)) // tick c:3
	_, _ = m.Update(keyRune('k'))             // -> b:2
	_, _ = m.Update(keyRune('k'))             // -> a:1
	_, _ = m.Update(keySpecial(tea.KeySpace)) // tick a:1

	sel := m.Selected()
	if len(sel) != 2 {
		t.Fatalf("Selected() = %d items, want 2: %+v", len(sel), sel)
	}
	if sel[0].Ref != "a:1" || sel[1].Ref != "c:3" {
		t.Errorf("Selected() order = [%s, %s], want [a:1, c:3]", sel[0].Ref, sel[1].Ref)
	}
}

// TestPicker_FormatAgo_TableDriven covers the human-friendly age buckets,
// including singular/plural flips. All cases run against a fixed clock.
func TestPicker_FormatAgo_TableDriven(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	clock := pickerFixedClock(now)

	cases := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"30s -> just now", 30 * time.Second, "just now"},
		{"just under 1 min -> just now", 59 * time.Second, "just now"},
		{"5 min", 5 * time.Minute, "5 min ago"},
		{"1 hour", 1 * time.Hour, "1 hour ago"},
		{"2 hours", 2 * time.Hour, "2 hours ago"},
		{"1 day", 26 * time.Hour, "1 day ago"},
		{"3 days", 3 * 24 * time.Hour, "3 days ago"},
		{"3 weeks", 3 * 7 * 24 * time.Hour, "3 weeks ago"},
		{"1 year", 400 * 24 * time.Hour, "1 year ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := now.Add(-tc.ago)
			got := formatAgo(ts, clock)
			if got != tc.want {
				t.Errorf("formatAgo(now - %v) = %q, want %q", tc.ago, got, tc.want)
			}
		})
	}
}

// TestPicker_ViewShowsHeaderAndHints — smoke test that View() includes
// the destination heading and the key-hint footer.
func TestPicker_ViewShowsHeaderAndHints(t *testing.T) {
	t.Parallel()
	m := newPushPickerModelWithHeader(
		[]DaemonImage{pickerAt(10, "a:1")},
		"vccr.io/proj",
	)
	m.now = pickerFixedClock(time.Unix(1_700_000_100, 0))
	out := m.View().Content
	if !strings.Contains(out, "Select images to push to vccr.io/proj") {
		t.Errorf("view missing header:\n%s", out)
	}
	if !strings.Contains(out, "Space:") || !strings.Contains(out, "Enter:") {
		t.Errorf("view missing key hints:\n%s", out)
	}
	if !strings.Contains(out, "a:1") {
		t.Errorf("view missing item row:\n%s", out)
	}
}
