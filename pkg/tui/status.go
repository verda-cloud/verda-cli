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

import "context"

// Status provides output/feedback components for showing progress during
// async operations. Unlike Prompter (which collects input), Status shows
// animated output while work happens in the background.
type Status interface {
	// Spinner starts an animated spinner with a message. Returns a handle
	// to update the message or stop the spinner. The spinner runs until
	// Stop() is called or the context is cancelled.
	Spinner(ctx context.Context, message string, opts ...SpinnerOption) (SpinnerHandle, error)

	// Progress starts an animated progress bar. Returns a handle to update
	// the percentage or stop. The bar runs until Stop() is called, the
	// percentage reaches 1.0, or the context is cancelled.
	Progress(ctx context.Context, message string, opts ...ProgressOption) (ProgressHandle, error)

	// Table renders a static table to the output and returns.
	Table(ctx context.Context, columns []string, rows [][]string, opts ...TableOption) error

	// Pager displays content in a scrollable viewport if it overflows the
	// terminal height, or prints it directly if it fits. The user navigates
	// with arrow keys/j/k/pgup/pgdn and exits with q/esc.
	Pager(ctx context.Context, content string, opts ...PagerOption) error
}

// SpinnerHandle controls a running spinner.
type SpinnerHandle interface {
	// UpdateMessage changes the spinner's status text.
	UpdateMessage(msg string)

	// Stop stops the spinner and shows a final message.
	// If finalMessage is empty, the last message is shown.
	Stop(finalMessage string)
}

// ProgressHandle controls a running progress bar.
type ProgressHandle interface {
	// SetPercent sets the progress bar to a value between 0.0 and 1.0.
	SetPercent(p float64)

	// Increment adds to the current percentage.
	Increment(delta float64)

	// Stop stops the progress bar and shows a final message.
	Stop(finalMessage string)
}
