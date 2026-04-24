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
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// pickerItem is one row in the push picker. Derived from DaemonImage minus
// fields the UI doesn't need. Dangling images (all RepoTags entries are
// "<none>:<none>") are filtered out by the constructor — the picker never
// shows them.
type pickerItem struct {
	Ref     string
	Size    int64
	Created time.Time
}

// pickerPageSize is the cursor step for PageUp/PageDown. Not directly tied
// to terminal height — we just want a useful jump. Exported as a const so
// tests can reference it if needed.
const pickerPageSize = 10

// danglingTag is the daemon sentinel tag for images with no user-visible
// name. Expanded dangling rows would be un-pushable so we skip them.
const danglingTag = "<none>:<none>"

// pushPickerModel is a bubbletea model for multi-selecting pickerItems.
//
// Pure: no network, no file I/O, no goroutines. All state is mutated
// in-place from tea.Msg handling. Tests drive it by calling Update()
// directly with synthetic KeyPressMsg values.
type pushPickerModel struct {
	// header is the "Select images to push to ..." line.
	header string

	items    []pickerItem
	selected map[int]bool // key = index into items

	cursor    int
	filter    string // empty when not filtering
	filtering bool   // true while user is typing a filter
	submitted bool   // true when user pressed Enter
	canceled  bool   // true when user pressed Esc / Ctrl+C

	width  int // terminal width (via tea.WindowSizeMsg)
	height int

	// now is the clock used by View() for age formatting. Swappable so
	// tests can assert deterministic "5 min ago" output.
	now func() time.Time
}

// newPushPickerModel constructs the model from a DaemonImage list. See
// newPushPickerModelWithHeader for header customization. Header defaults
// to the bare "Select images to push".
func newPushPickerModel(images []DaemonImage) *pushPickerModel {
	return newPushPickerModelWithHeader(images, "")
}

// newPushPickerModelWithHeader constructs the model and sets the header
// to "Select images to push to <dst>". Pass an empty dst to get the
// bare heading (useful in tests).
//
// Rules applied to the daemon list:
//   - Dangling images (no RepoTags, or all RepoTags == "<none>:<none>") are
//     skipped.
//   - Multi-tag images expand to one pickerItem per non-dangling tag.
//   - Items are sorted by Created descending (newest first). The sort is
//     stable so the tag order within a multi-tag image is preserved when
//     Created ties.
func newPushPickerModelWithHeader(images []DaemonImage, dst string) *pushPickerModel {
	items := make([]pickerItem, 0, len(images))
	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == "" || tag == danglingTag {
				continue
			}
			items = append(items, pickerItem{
				Ref:     tag,
				Size:    img.Size,
				Created: img.Created,
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Created.After(items[j].Created)
	})

	header := "Select images to push"
	if dst != "" {
		header = fmt.Sprintf("Select images to push to %s:", dst)
	}

	return &pushPickerModel{
		header:   header,
		items:    items,
		selected: make(map[int]bool),
		now:      time.Now,
	}
}

// Selected returns the chosen items in display order (i.e. Created desc,
// same as View). Empty if the user canceled or nothing was ticked.
// Safe to call after Run() has returned.
func (m *pushPickerModel) Selected() []pickerItem {
	if m.canceled {
		return nil
	}
	out := make([]pickerItem, 0, len(m.selected))
	for i, it := range m.items {
		if m.selected[i] {
			out = append(out, it)
		}
	}
	return out
}

// Canceled reports whether the user aborted (Esc / Ctrl+C).
func (m *pushPickerModel) Canceled() bool { return m.canceled }

// Init satisfies tea.Model. No startup command needed; the picker is
// entirely event-driven.
func (m *pushPickerModel) Init() tea.Cmd { return nil }

// Update dispatches tea messages. The picker only cares about key presses
// and window-size events; everything else passes through unchanged.
func (m *pushPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if m.filtering {
			return m.updateFilterMode(msg)
		}
		return m.updateNormalMode(msg)
	}
	return m, nil
}

// isCtrlC matches a Ctrl+C key press via either the modifier check or the
// string form documented in the v2 upgrade guide. Matches push_view.go.
func isCtrlC(msg tea.KeyPressMsg) bool {
	return (msg.Code == 'c' && msg.Mod == tea.ModCtrl) || msg.String() == "ctrl+c"
}

// updateNormalMode handles key presses while NOT typing a filter.
func (m *pushPickerModel) updateNormalMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if isCtrlC(msg) {
		m.canceled = true
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEsc:
		m.canceled = true
		return m, tea.Quit
	case tea.KeyEnter:
		m.submitted = true
		return m, tea.Quit
	case tea.KeyUp:
		m.moveCursor(-1)
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		m.moveCursor(+1)
		return m, nil
	case tea.KeyPgUp:
		m.moveCursor(-pickerPageSize)
		return m, nil
	case tea.KeyPgDown:
		m.moveCursor(+pickerPageSize)
		return m, nil
	case tea.KeyBackspace:
		// No-op outside filter mode.
		return m, nil
	case tea.KeySpace:
		m.toggleSelectionAtCursor()
		return m, nil
	}

	// Shift+Tab (cursor up) — Mod check since KeyTab + ModShift is what
	// xterm emits for reverse-tab.
	if msg.Code == tea.KeyTab && msg.Mod.Contains(tea.ModShift) {
		m.moveCursor(-1)
		return m, nil
	}

	// Printable-rune bindings. These come in as msg.Code == 'j' etc.
	switch msg.Code {
	case 'j':
		m.moveCursor(+1)
	case 'k':
		m.moveCursor(-1)
	case ' ':
		// Some terminals deliver space as a rune rather than KeySpace.
		m.toggleSelectionAtCursor()
	case 'a':
		m.toggleAllVisible()
	case 'A':
		m.invertVisible()
	case '/':
		m.filtering = true
		m.filter = ""
	}
	return m, nil
}

// updateFilterMode handles key presses while the user is typing into the
// filter bar.
func (m *pushPickerModel) updateFilterMode(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always cancels, even in filter mode.
	if isCtrlC(msg) {
		m.canceled = true
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEnter:
		// Commit the filter. Park the cursor on the first visible row so
		// subsequent nav keys feel natural.
		m.filtering = false
		m.cursor = m.firstVisibleIndex()
		return m, nil
	case tea.KeyEsc:
		// Drop filter entirely.
		m.filtering = false
		m.filter = ""
		return m, nil
	case tea.KeyBackspace:
		if m.filter != "" {
			// Rune-safe trim of the last char so multi-byte filters edit cleanly.
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
		}
		return m, nil
	}

	// Any printable text is appended. msg.Text holds the actual rune(s)
	// for plain keys; fall back to msg.Code when Text is empty (some
	// terminals only set Code for simple ASCII input).
	if msg.Text != "" {
		m.filter += msg.Text
		return m, nil
	}
	if msg.Code > 0 && msg.Code < 0x110000 && isPrintableRune(msg.Code) {
		m.filter += string(msg.Code)
	}
	return m, nil
}

// isPrintableRune reports whether r is a reasonable character to allow in
// a filter string. Excludes control chars; accepts spaces.
func isPrintableRune(r rune) bool {
	return r >= 0x20 && r != 0x7f
}

// moveCursor moves by delta across the VISIBLE rows, clamping at the ends.
// Movement is expressed in visible-row space so filtering feels natural.
func (m *pushPickerModel) moveCursor(delta int) {
	visible := m.visibleIndexes()
	if len(visible) == 0 {
		return
	}
	// Find the current cursor's position in visible-space (or clamp to 0).
	pos := 0
	for i, idx := range visible {
		if idx == m.cursor {
			pos = i
			break
		}
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(visible) {
		pos = len(visible) - 1
	}
	m.cursor = visible[pos]
}

// toggleSelectionAtCursor flips selection on the item at the cursor,
// provided it's currently visible. A cursor sitting on a filtered-out row
// is a no-op — we never silently mutate hidden rows.
func (m *pushPickerModel) toggleSelectionAtCursor() {
	if !m.isVisible(m.cursor) {
		return
	}
	if m.selected[m.cursor] {
		delete(m.selected, m.cursor)
	} else {
		m.selected[m.cursor] = true
	}
}

// toggleAllVisible is the `a` binding: if any visible row is unselected,
// select all visible rows; otherwise deselect all visible. Only acts on
// visible rows so a filter narrows the scope of mass toggles.
func (m *pushPickerModel) toggleAllVisible() {
	visible := m.visibleIndexes()
	if len(visible) == 0 {
		return
	}
	hasUnselected := false
	for _, i := range visible {
		if !m.selected[i] {
			hasUnselected = true
			break
		}
	}
	if hasUnselected {
		for _, i := range visible {
			m.selected[i] = true
		}
	} else {
		for _, i := range visible {
			delete(m.selected, i)
		}
	}
}

// invertVisible is the `A` binding: flip selection state for every visible
// row.
func (m *pushPickerModel) invertVisible() {
	for _, i := range m.visibleIndexes() {
		if m.selected[i] {
			delete(m.selected, i)
		} else {
			m.selected[i] = true
		}
	}
}

// visibleIndexes returns indices into m.items whose Ref passes the current
// case-insensitive substring filter. When the filter is empty, returns all
// indices. Order matches m.items (which is already Created-desc).
func (m *pushPickerModel) visibleIndexes() []int {
	out := make([]int, 0, len(m.items))
	filter := strings.ToLower(m.filter)
	for i, it := range m.items {
		if filter == "" || strings.Contains(strings.ToLower(it.Ref), filter) {
			out = append(out, i)
		}
	}
	return out
}

// isVisible is a narrow check against the current filter, used by cursor
// operations that must not act on hidden rows.
func (m *pushPickerModel) isVisible(i int) bool {
	if i < 0 || i >= len(m.items) {
		return false
	}
	if m.filter == "" {
		return true
	}
	return strings.Contains(
		strings.ToLower(m.items[i].Ref),
		strings.ToLower(m.filter),
	)
}

// firstVisibleIndex is the cursor landing spot after committing a filter.
// Falls back to 0 if nothing matches (harmless — View will just render an
// empty list).
func (m *pushPickerModel) firstVisibleIndex() int {
	v := m.visibleIndexes()
	if len(v) == 0 {
		return 0
	}
	return v[0]
}

// --- View ---

var (
	pickerStyleHeading   = lipgloss.NewStyle().Bold(true)
	pickerStyleCursor    = lipgloss.NewStyle().Bold(true)
	pickerStyleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	pickerStyleSelected  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	pickerStyleFilterHdr = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

const (
	pickerRefColWidth  = 32
	pickerSizeColWidth = 10
	pickerCursorMark   = "\u276f" // ❯
	pickerCursorBlank  = " "
)

// View renders the picker. Pure — output depends only on model state and
// m.now() for age formatting.
func (m *pushPickerModel) View() tea.View {
	var b strings.Builder

	b.WriteString(pickerStyleHeading.Render(m.header))
	b.WriteString("\n\n")

	visible := m.visibleIndexes()
	if len(visible) == 0 {
		b.WriteString(pickerStyleDim.Render("  (no images match filter)"))
		b.WriteString("\n")
	}
	for _, i := range visible {
		b.WriteString(m.renderRow(i))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.filtering {
		b.WriteString(pickerStyleFilterHdr.Render(
			fmt.Sprintf("Filter: %s_   (Esc to clear, Enter to confirm)", m.filter),
		))
	} else {
		b.WriteString(pickerStyleDim.Render(
			"Space: toggle \u00b7 a: all \u00b7 A: invert \u00b7 /: filter \u00b7 Enter: continue \u00b7 Esc: cancel",
		))
	}
	b.WriteString("\n")

	return tea.NewView(b.String())
}

// renderRow produces one line for a single item. Pure.
func (m *pushPickerModel) renderRow(i int) string {
	item := m.items[i]
	cursor := pickerCursorBlank
	if i == m.cursor {
		cursor = pickerStyleCursor.Render(pickerCursorMark)
	}
	mark := "[ ]"
	if m.selected[i] {
		mark = pickerStyleSelected.Render("[x]")
	}
	ref := padRef(item.Ref, pickerRefColWidth)
	size := rightPad(formatBytes(item.Size), pickerSizeColWidth)
	age := formatAgo(item.Created, m.now)

	return fmt.Sprintf("%s %s %s  %s  %s", cursor, mark, ref, size, age)
}

// rightPad right-aligns s in a w-rune column by left-padding with spaces.
// Mirrors padRef (which left-aligns) so View() columns line up regardless
// of size-string length.
func rightPad(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(r)) + s
}

// formatAgo renders a "time since" string in a human-readable short form.
// The now function is injected so tests can assert deterministic output
// without touching the real clock.
//
// Format ladder:
//
//	< 60s           -> "just now"
//	< 60min         -> "N min ago"
//	< 24h           -> "N hour[s] ago"
//	< 7 days        -> "N day[s] ago"
//	< 4 weeks       -> "N week[s] ago"
//	< 12 months     -> "N month[s] ago"
//	otherwise       -> "N year[s] ago"
//
// Negative durations (future timestamps, clock skew) clamp to "just now".
func formatAgo(t time.Time, now func() time.Time) string {
	if now == nil {
		now = time.Now
	}
	d := now().Sub(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		n := int(d / time.Minute)
		return fmt.Sprintf("%d min ago", n)
	}
	if d < 24*time.Hour {
		n := int(d / time.Hour)
		return pluralAgo(n, "hour")
	}
	if d < 7*24*time.Hour {
		n := int(d / (24 * time.Hour))
		return pluralAgo(n, "day")
	}
	if d < 4*7*24*time.Hour {
		n := int(d / (7 * 24 * time.Hour))
		return pluralAgo(n, "week")
	}
	// Use 30-day months for rough-but-stable output. A year is 365 days.
	if d < 365*24*time.Hour {
		n := int(d / (30 * 24 * time.Hour))
		if n < 1 {
			n = 1
		}
		return pluralAgo(n, "month")
	}
	n := int(d / (365 * 24 * time.Hour))
	if n < 1 {
		n = 1
	}
	return pluralAgo(n, "year")
}

// pluralAgo formats a "<n> <unit>[s] ago" line, picking singular vs plural
// so "1 hour ago" and "2 hours ago" both read naturally.
func pluralAgo(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}
