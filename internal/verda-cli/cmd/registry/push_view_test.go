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
	"bytes"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// newTestRows constructs n queued rows for bubbletea-model tests.
func newTestRows(refs ...string) []imageRow {
	rows := make([]imageRow, len(refs))
	for i, r := range refs {
		rows[i] = imageRow{Ref: r, State: stateQueued}
	}
	return rows
}

// cmdsEqual compares two tea.Cmd functions by invoking them and comparing
// the resulting tea.Msg types. tea.Quit is returned by the model when the
// push completes, so this predicate is enough to assert quit semantics.
func cmdIsQuit(c tea.Cmd) bool {
	if c == nil {
		return false
	}
	msg := c()
	if msg == nil {
		return false
	}
	// tea.Quit() returns a QuitMsg; compare by type name so tests don't
	// break if the internal struct gains fields.
	return reflect.TypeOf(msg).String() == "tea.QuitMsg"
}

// TestPushView_InitialStateAllQueued: freshly built model renders every ref
// with a "queued" marker.
func TestPushView_InitialStateAllQueued(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1", "beta:v2", "gamma:v3")
	m := newPushViewModel("vccr.io", "proj", rows, func() {})

	out := m.View().Content
	for _, ref := range []string{"alpha:v1", "beta:v2", "gamma:v3"} {
		if !strings.Contains(out, ref) {
			t.Errorf("View missing ref %q:\n%s", ref, out)
		}
	}
	if !strings.Contains(out, "queued") {
		t.Errorf("expected 'queued' marker in view:\n%s", out)
	}
	if !strings.Contains(out, "Pushing 3 images to vccr.io/proj") {
		t.Errorf("expected heading in view:\n%s", out)
	}
}

// TestPushView_ProgressTransitionsState: a progress msg on a queued row
// should promote it to in-flight and record a meter tick.
func TestPushView_ProgressTransitionsState(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1", "beta:v2", "gamma:v3")
	m := newPushViewModel("h", "p", rows, func() {})

	_, _ = m.Update(pushProgressMsg{
		Index:  1,
		Update: v1.Update{Complete: 1000, Total: 10000},
	})

	got := m.rows[1]
	if got.State != stateInFlight {
		t.Errorf("rows[1].State = %v, want stateInFlight", got.State)
	}
	if got.Meter == nil {
		t.Fatal("rows[1].Meter is nil after progress msg")
	}
	snap := got.Meter.Snapshot()
	if snap.Complete != 1000 || snap.Total != 10000 {
		t.Errorf("snapshot = %+v, want Complete=1000 Total=10000", snap)
	}

	// Other rows stay queued.
	if m.rows[0].State != stateQueued || m.rows[2].State != stateQueued {
		t.Errorf("other rows changed state unexpectedly: [0]=%v [2]=%v",
			m.rows[0].State, m.rows[2].State)
	}
}

// TestPushView_CompletionMarksDone: a nil-error result promotes the row
// to Done; the view shows the success marker and a 'pushed' word.
func TestPushView_CompletionMarksDone(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1", "beta:v2")
	m := newPushViewModel("h", "p", rows, func() {})

	// Give alpha a meter first so DoneBytes/Took are non-zero.
	baseTime := time.Unix(1_700_000_000, 0)
	alphaMeter := &Meter{now: fakeClock(baseTime, baseTime.Add(1*time.Second))}
	alphaMeter.Update(500, 1000)
	alphaMeter.Update(1000, 1000)
	m.rows[0].Meter = alphaMeter
	m.rows[0].State = stateInFlight

	_, _ = m.Update(pushResultMsg{Index: 0, Err: nil})

	if m.rows[0].State != stateDone {
		t.Errorf("rows[0].State = %v, want stateDone", m.rows[0].State)
	}
	if m.rows[0].Meter != nil {
		t.Errorf("expected meter cleared on done, got %+v", m.rows[0].Meter)
	}
	if m.rows[0].DoneBytes != 1000 {
		t.Errorf("DoneBytes = %d, want 1000", m.rows[0].DoneBytes)
	}

	out := m.View().Content
	if !strings.Contains(out, pushMarkerDone) {
		t.Errorf("view missing done marker %q:\n%s", pushMarkerDone, out)
	}
	if !strings.Contains(out, "pushed") {
		t.Errorf("view missing 'pushed' word:\n%s", out)
	}
}

// TestPushView_FailureMarksFailed: a result msg with a non-nil error
// transitions to Failed; the error text shows up in the view.
func TestPushView_FailureMarksFailed(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1", "beta:v2")
	m := newPushViewModel("h", "p", rows, func() {})

	boom := errors.New("boom")
	_, _ = m.Update(pushResultMsg{Index: 0, Err: boom})

	if m.rows[0].State != stateFailed {
		t.Errorf("rows[0].State = %v, want stateFailed", m.rows[0].State)
	}
	if !errors.Is(m.rows[0].Err, boom) {
		t.Errorf("rows[0].Err = %v, want boom", m.rows[0].Err)
	}

	out := m.View().Content
	if !strings.Contains(out, pushMarkerFailed) {
		t.Errorf("view missing failed marker %q:\n%s", pushMarkerFailed, out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("view missing error text 'boom':\n%s", out)
	}
}

// TestPushView_AllDoneQuits: after every row receives a terminal result,
// the next Update returns tea.Quit and m.finished is true.
func TestPushView_AllDoneQuits(t *testing.T) {
	t.Parallel()
	rows := newTestRows("a:1", "b:2", "c:3")
	m := newPushViewModel("h", "p", rows, func() {})

	// First two succeed.
	_, c0 := m.Update(pushResultMsg{Index: 0, Err: nil})
	_, c1 := m.Update(pushResultMsg{Index: 1, Err: nil})
	if cmdIsQuit(c0) || cmdIsQuit(c1) {
		t.Fatal("model should not quit before all rows finish")
	}
	if m.finished {
		t.Fatal("m.finished set before last result")
	}

	_, cLast := m.Update(pushResultMsg{Index: 2, Err: errors.New("boom")})
	if !m.finished {
		t.Error("m.finished is false after last result")
	}
	if !cmdIsQuit(cLast) {
		t.Error("expected tea.Quit after last result")
	}
}

// TestPushView_CtrlCInvokesCancelAndQuits: a Ctrl-C key press invokes the
// ctxDone cancel function once and asks the program to quit.
func TestPushView_CtrlCInvokesCancelAndQuits(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1")
	called := 0
	m := newPushViewModel("h", "p", rows, func() { called++ })

	key := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	_, cmd := m.Update(key)
	if called != 1 {
		t.Errorf("ctxDone called %d times, want 1", called)
	}
	if !cmdIsQuit(cmd) {
		t.Errorf("expected tea.Quit, got %T", cmd)
	}

	// Second Ctrl-C is idempotent (ctxDone cleared).
	_, cmd2 := m.Update(key)
	if called != 1 {
		t.Errorf("ctxDone called %d times after 2x Ctrl-C, want 1", called)
	}
	if !cmdIsQuit(cmd2) {
		t.Errorf("second Ctrl-C must still quit; got %T", cmd2)
	}
}

// TestPushView_ViewShowsThroughputAndETA: with an injected fake clock the
// in-flight row should show a throughput token ("B/s", "KiB/s", ...) and
// an ETA token.
func TestPushView_ViewShowsThroughputAndETA(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1", "beta:v2")
	m := newPushViewModel("h", "p", rows, func() {})

	// Replace the row's meter with a clock-controlled one so the snapshot
	// yields a deterministic throughput + ETA.
	base := time.Unix(1_700_000_000, 0)
	meter := &Meter{now: fakeClock(
		base,
		base.Add(1*time.Second),
	)}
	meter.Update(0, 10_000_000)
	meter.Update(1_000_000, 10_000_000) // 1 MB in 1s -> ~1 MB/s
	m.rows[0].Meter = meter
	m.rows[0].State = stateInFlight

	out := m.View().Content

	// Throughput token: one of the units the formatter can produce.
	hasThroughput := strings.Contains(out, "B/s") ||
		strings.Contains(out, "KiB/s") ||
		strings.Contains(out, "MiB/s") ||
		strings.Contains(out, "GiB/s")
	if !hasThroughput {
		t.Errorf("view missing throughput suffix:\n%s", out)
	}
	if !strings.Contains(out, "ETA") {
		t.Errorf("view missing ETA token:\n%s", out)
	}
}

// TestShouldUseBubbletea_Table exhaustively checks the decision matrix.
// We override isTerminalFn so the pass/fail doesn't depend on whether the
// test runner's stderr happens to be a TTY.
func TestShouldUseBubbletea_Table(t *testing.T) {
	prev := isTerminalFn
	t.Cleanup(func() { isTerminalFn = prev })

	cases := []struct {
		name       string
		progress   string
		output     string
		isTerminal bool
		want       bool
	}{
		{"auto+table+tty", "auto", "table", true, true},
		{"auto+table+notty", "auto", "table", false, false},
		{"auto+json+tty", "auto", "json", true, false},
		{"auto+yaml+tty", "auto", "yaml", true, false},
		{"plain+table+tty", "plain", "table", true, false},
		{"none+table+tty", "none", "table", true, false},
		{"json+table+tty", "json", "table", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isTerminalFn = func(io.Writer) bool { return tc.isTerminal }
			opts := &pushOptions{Progress: tc.progress}
			got := shouldUseBubbletea(opts, tc.output, io.Discard)
			if got != tc.want {
				t.Errorf("shouldUseBubbletea(progress=%s output=%s tty=%v) = %v, want %v",
					tc.progress, tc.output, tc.isTerminal, got, tc.want)
			}
		})
	}
}

// TestRenderProgressBar covers the boundary fractions and a mid-range
// fraction. Width 10, 0.5 -> 5 filled + 5 unfilled.
func TestRenderProgressBar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		fraction float64
		width    int
		filled   int
		unfilled int
	}{
		{"half", 0.5, 10, 5, 5},
		{"zero", 0, 10, 0, 10},
		{"one", 1, 10, 10, 0},
		{"negative clamps to zero", -0.1, 8, 0, 8},
		{"over-one clamps to full", 1.5, 8, 8, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderProgressBar(tc.fraction, tc.width)
			gotFilled := strings.Count(got, pushBarFilled)
			gotUnfilled := strings.Count(got, pushBarUnfilled)
			if gotFilled != tc.filled {
				t.Errorf("filled = %d, want %d (out=%q)", gotFilled, tc.filled, got)
			}
			if gotUnfilled != tc.unfilled {
				t.Errorf("unfilled = %d, want %d (out=%q)", gotUnfilled, tc.unfilled, got)
			}
		})
	}
}

// TestFormatMMSS: common durations + the hour boundary.
func TestFormatMMSS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "00:00"},
		{"42s", 42 * time.Second, "00:42"},
		{"99s", 99 * time.Second, "01:39"},
		{"1h exact", 1 * time.Hour, "01:00:00"},
		{"1h23m4s", 1*time.Hour + 23*time.Minute + 4*time.Second, "01:23:04"},
		{"negative clamps", -5 * time.Second, "00:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatMMSS(tc.d)
			if got != tc.want {
				t.Errorf("formatMMSS(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestPushView_LateProgressIgnoredAfterResult guards against the race where
// ggcr emits a straggling v1.Update after the result msg has already been
// dispatched. Such a progress msg must not resurrect the row to in-flight.
func TestPushView_LateProgressIgnoredAfterResult(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1")
	m := newPushViewModel("h", "p", rows, func() {})

	_, _ = m.Update(pushResultMsg{Index: 0, Err: nil})
	_, _ = m.Update(pushProgressMsg{Index: 0, Update: v1.Update{Complete: 1, Total: 1}})

	if m.rows[0].State != stateDone {
		t.Errorf("state flipped back to %v after late progress", m.rows[0].State)
	}
	if m.rows[0].Meter != nil {
		t.Errorf("meter revived by late progress msg: %+v", m.rows[0].Meter)
	}
}

// TestPushView_TickReschedules: the tick handler must return a command that
// re-arms itself so throughput updates continue when ggcr goes quiet.
func TestPushView_TickReschedules(t *testing.T) {
	t.Parallel()
	rows := newTestRows("alpha:v1")
	m := newPushViewModel("h", "p", rows, func() {})

	_, cmd := m.Update(pushTickMsg{})
	if cmd == nil {
		t.Fatal("tick returned nil command; view would freeze")
	}
	// Don't actually invoke the tick (it would sleep for pushTickInterval);
	// asserting non-nil + the model state unchanged is enough.
	if m.rows[0].State != stateQueued {
		t.Errorf("tick mutated state to %v", m.rows[0].State)
	}
}

// TestIsTerminalFD_NonFile guards against false positives: a bytes.Buffer
// is never a terminal, regardless of host.
func TestIsTerminalFD_NonFile(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if isTerminalFD(&buf) {
		t.Error("isTerminalFD(*bytes.Buffer) = true, want false")
	}
}

// TestFormatBytes_Shared sanity-checks that formatBytes behaves the same as
// the old formatTagBytes for the table renderer.
func TestFormatBytes_Shared(t *testing.T) {
	t.Parallel()
	cases := []struct {
		n    int64
		want string
	}{
		{-1, "--"},
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KiB"},
		{1536, "1.50 KiB"},
	}
	for _, tc := range cases {
		t.Run(strconv.FormatInt(tc.n, 10), func(t *testing.T) {
			if got := formatBytes(tc.n); got != tc.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}
