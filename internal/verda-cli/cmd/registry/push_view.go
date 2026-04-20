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
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/charmbracelet/x/term"
)

// imageState is the lifecycle state of a single image row in the push view.
type imageState int

const (
	stateQueued   imageState = iota // not yet started
	stateInFlight                   // at least one progress tick received
	stateDone                       // completed successfully
	stateFailed                     // completed with an error
)

// imageRow is one line in the push progress view. A row carries its own
// Meter so throughput/ETA per image can be derived without consulting the
// global clock inside View().
type imageRow struct {
	Ref       string
	Dst       string
	State     imageState
	Meter     *Meter
	Err       error
	Took      time.Duration // total elapsed at Done
	DoneBytes int64         // total transferred at Done
}

// pushProgressMsg delivers a ggcr v1.Update for a specific image row.
type pushProgressMsg struct {
	Index  int
	Update v1.Update
}

// pushResultMsg signals a row has finished (success or failure).
type pushResultMsg struct {
	Index int
	Err   error
}

// pushTickMsg is the self-scheduled tick that keeps throughput/ETA fresh
// between ggcr progress updates.
type pushTickMsg struct{}

// pushHeaderNoteMsg updates the optional sub-heading rendered between the
// main heading and the row list. copy --all-tags uses this to show a live
// "copied K of N tags" summary as individual tag copies complete.
type pushHeaderNoteMsg struct {
	Note string
}

// pushTickInterval is how often the view re-renders independently of ggcr
// progress. 100ms is smooth to the eye without burning CPU.
const pushTickInterval = 100 * time.Millisecond

// pushViewModel is the bubbletea model for `verda registry push` in
// auto+TTY mode. Pure: View never touches a clock; all throughput/ETA
// values flow from per-row Meter.Snapshot().
type pushViewModel struct {
	host       string
	project    string
	rows       []imageRow
	ctxDone    func() // invoked on Ctrl-C to cancel the upstream push context
	finished   bool   // true once every row is Done or Failed
	headerNote string // optional sub-heading (e.g. "copied K of N tags"); copy --all-tags only
}

// newPushViewModel builds a fresh model with every row queued.
func newPushViewModel(host, project string, rows []imageRow, ctxDone func()) *pushViewModel {
	return &pushViewModel{
		host:    host,
		project: project,
		rows:    rows,
		ctxDone: ctxDone,
	}
}

// Init returns the first tick command. Without this the view would freeze
// between ggcr progress updates (which can be seconds apart on slow uploads).
func (m *pushViewModel) Init() tea.Cmd {
	return pushTickCmd()
}

func pushTickCmd() tea.Cmd {
	return tea.Tick(pushTickInterval, func(time.Time) tea.Msg { return pushTickMsg{} })
}

// Update is the dispatch function. Keeps allocation tight: returns a tea.Cmd
// only for quit / re-arm / initial tick cases. Per-message handling is
// extracted into helpers so each case stays small and the function's
// cyclomatic complexity stays under the linter cap.
func (m *pushViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pushProgressMsg:
		m.handleProgress(msg)
		return m, nil

	case pushResultMsg:
		if cmd := m.handleResult(msg); cmd != nil {
			return m, cmd
		}
		return m, nil

	case pushTickMsg:
		return m, pushTickCmd()

	case pushHeaderNoteMsg:
		m.headerNote = msg.Note
		return m, nil

	case tea.KeyPressMsg:
		cmd := m.handleKey(msg)
		return m, cmd
	}
	return m, nil
}

// handleProgress applies a ggcr progress tick to the target row's meter,
// transitioning queued -> in-flight on the first update. Terminal-state
// rows ignore late ticks — ggcr can deliver the last byte of a layer
// after the result msg has already landed.
func (m *pushViewModel) handleProgress(msg pushProgressMsg) {
	if msg.Index < 0 || msg.Index >= len(m.rows) {
		return
	}
	row := &m.rows[msg.Index]
	if row.State == stateDone || row.State == stateFailed {
		return
	}
	if row.State == stateQueued {
		row.State = stateInFlight
		row.Meter = &Meter{}
	}
	if row.Meter == nil {
		row.Meter = &Meter{}
	}
	row.Meter.Update(msg.Update.Complete, msg.Update.Total)
}

// handleResult finalizes a row's state after Write returns. Returns
// tea.Quit when every row has reached a terminal state, so the caller
// can surface the command to the outer Update return value.
func (m *pushViewModel) handleResult(msg pushResultMsg) tea.Cmd {
	if msg.Index < 0 || msg.Index >= len(m.rows) {
		return nil
	}
	row := &m.rows[msg.Index]
	if msg.Err != nil {
		row.State = stateFailed
		row.Err = msg.Err
	} else {
		row.State = stateDone
	}
	if row.Meter != nil {
		snap := row.Meter.Snapshot()
		row.Took = snap.Elapsed
		row.DoneBytes = snap.Complete
	}
	row.Meter = nil

	if m.allFinished() {
		m.finished = true
		return tea.Quit
	}
	return nil
}

// handleKey is the Ctrl-C cancel path. Matches via both the modifier
// check (matches wizard) and the string form documented in the v2
// upgrade guide — keeping both means future key-encoding refinements in
// v2 don't silently break the cancel path. A second Ctrl-C is a no-op.
func (m *pushViewModel) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if (msg.Code == 'c' && msg.Mod == tea.ModCtrl) || msg.String() == "ctrl+c" {
		if m.ctxDone != nil {
			m.ctxDone()
			m.ctxDone = nil
		}
		return tea.Quit
	}
	return nil
}

// allFinished reports whether every row has reached a terminal state.
func (m *pushViewModel) allFinished() bool {
	for i := range m.rows {
		if m.rows[i].State != stateDone && m.rows[i].State != stateFailed {
			return false
		}
	}
	return true
}

// --- styles ---
//
// Kept as package-level vars so View() stays allocation-light. Colors
// match cmd/volume/delete.go and cmd/status/status.go: ANSI 8 for dim,
// 2 for success, 1 for failure.
var (
	pushStyleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	pushStyleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	pushStyleFailed  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	pushStyleHeading = lipgloss.NewStyle().Bold(true)
)

const (
	pushBarWidth     = 16 // progress bar cells, not counting brackets
	pushRefColWidth  = 24 // minimum width of the ref column for alignment
	pushBarFilled    = "━"
	pushBarUnfilled  = "╺"
	pushMarkerQueue  = " "
	pushMarkerActive = "▸"
	pushMarkerDone   = "✓"
	pushMarkerFailed = "✗"
)

// View renders the progress view. Pure — depends only on model state plus
// each row's Meter.Snapshot(). The snapshot already encodes elapsed time
// relative to the meter's injected clock, so tests can drive throughput
// deterministically without patching time.Now on this package.
func (m *pushViewModel) View() tea.View {
	var b strings.Builder

	heading := fmt.Sprintf("Pushing %d image%s to %s/%s",
		len(m.rows), pluralS(len(m.rows)), m.host, m.project)
	b.WriteString(pushStyleHeading.Render(heading))
	b.WriteString("\n")

	// Optional sub-heading (copy --all-tags uses this to show a live
	// "copied K of N tags" summary). Dimmed so it reads as secondary.
	if m.headerNote != "" {
		b.WriteString(pushStyleDim.Render(m.headerNote))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	for i := range m.rows {
		b.WriteString(renderRow(&m.rows[i]))
		b.WriteString("\n")
	}

	return tea.NewView(b.String())
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// renderRow produces one line for a single image. Pure and allocation-
// local — callers can build the whole view with a single strings.Builder.
func renderRow(row *imageRow) string {
	ref := padRef(row.Ref, pushRefColWidth)

	switch row.State {
	case stateQueued:
		return pushStyleDim.Render(fmt.Sprintf("%s %s  queued",
			pushMarkerQueue, ref))

	case stateDone:
		line := fmt.Sprintf("%s %s  pushed (%s in %s)",
			pushMarkerDone, ref, formatBytes(row.DoneBytes), formatMMSS(row.Took))
		return pushStyleDone.Render(line)

	case stateFailed:
		errMsg := "unknown error"
		if row.Err != nil {
			errMsg = row.Err.Error()
		}
		line := fmt.Sprintf("%s %s  failed: %s", pushMarkerFailed, ref, errMsg)
		return pushStyleFailed.Render(line)

	case stateInFlight:
		// Defensive: if an in-flight row somehow has no meter, render a
		// minimal line rather than panicking.
		if row.Meter == nil {
			return fmt.Sprintf("%s %s  pushing...", pushMarkerActive, ref)
		}
		snap := row.Meter.Snapshot()
		bar := renderProgressBar(snap.Fraction, pushBarWidth)
		pct := int(snap.Fraction*100 + 0.5)
		bytesStr := formatBytes(snap.Complete) + " / " + formatBytes(snap.Total)
		tputStr := formatBytesPerSec(snap.ThroughputBps)
		etaStr := "ETA --:--"
		if snap.ETA > 0 {
			etaStr = "ETA " + formatMMSS(snap.ETA)
		}
		return fmt.Sprintf("%s %s  %s  %3d%%  %s  %s  %s",
			pushMarkerActive, ref, bytesStr, pct, bar, tputStr, etaStr)
	}

	// Unreachable — all states are covered above.
	return ref
}

// padRef pads (or truncates) a ref to width runes so the per-image columns
// align. Uses rune-aware length so multi-byte repository names don't
// break alignment.
func padRef(ref string, width int) string {
	runes := []rune(ref)
	if len(runes) >= width {
		return ref
	}
	return ref + strings.Repeat(" ", width-len(runes))
}

// renderProgressBar draws a bar of exactly `width` cells, using ━ for
// filled cells and ╺ for unfilled ones. Fractions outside [0,1] clamp.
// Pure.
func renderProgressBar(fraction float64, width int) string {
	if width <= 0 {
		return ""
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(float64(width)*fraction + 0.0001) // tiny epsilon so 1.0 -> width
	if filled > width {
		filled = width
	}
	var b strings.Builder
	b.Grow(width * len(pushBarFilled)) // approximate
	for i := 0; i < filled; i++ {
		b.WriteString(pushBarFilled)
	}
	for i := filled; i < width; i++ {
		b.WriteString(pushBarUnfilled)
	}
	return b.String()
}

// --- TTY / progress-flag decision ---

// isTerminalFn is the real TTY probe, swappable by tests.
var isTerminalFn = isTerminalFD

// isTerminalFD reports whether w is an *os.File attached to a terminal.
// Non-file writers (buffers in tests, pipe stdout under CI) always return
// false, which matches the intent of the --progress=auto switch.
func isTerminalFD(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(f.Fd())
}

// shouldUseBubbletea reports whether the auto+TTY bubbletea view should be
// chosen over the flat-text fallback. The decision matrix:
//
//	progress flag   structured output?   TTY?   -> bubbletea?
//	--------------------------------------------------------
//	none            any                  any    -> no
//	plain           any                  any    -> no
//	json            any                  any    -> no
//	auto            json/yaml            any    -> no
//	auto            table                no     -> no
//	auto            table                yes    -> YES
func shouldUseBubbletea(opts *pushOptions, outputFormat string, errOut io.Writer) bool {
	switch opts.Progress {
	case progressNone, progressPlain, progressJSON:
		return false
	}
	if isStructuredFormat(outputFormat) {
		return false
	}
	return isTerminalFn(errOut)
}
