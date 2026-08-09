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
	"fmt"
	"reflect"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

// ProgressViewOption configures a ProgressView.
type ProgressViewOption func(*ProgressView)

// WithProgressGradient sets the gradient colors for the progress bar.
// Defaults to the bubbles default gradient (#5A56E0 -> #EE6FF8).
func WithProgressGradient(colorA, colorB string) ProgressViewOption {
	return func(r *ProgressView) {
		r.colorA = colorA
		r.colorB = colorB
	}
}

// WithProgressSolidFill uses a single color instead of a gradient.
func WithProgressSolidFill(color string) ProgressViewOption {
	return func(r *ProgressView) {
		r.solidFill = color
	}
}

// WithProgressWidth sets the bar width in characters (default: 40).
func WithProgressWidth(w int) ProgressViewOption {
	return func(r *ProgressView) {
		r.width = w
	}
}

// WithProgressPercent shows percentage text (e.g., "33%") instead of the
// default "Step X of Y" label. Follows the bubbletea animated progress example.
func WithProgressPercent() ProgressViewOption {
	return func(r *ProgressView) {
		r.showPercent = true
		r.hideStepLabel = true
	}
}

// WithoutProgressStepLabel hides the "Step X of Y" label.
func WithoutProgressStepLabel() ProgressViewOption {
	return func(r *ProgressView) {
		r.hideStepLabel = true
	}
}

// ProgressView displays an animated-style step progress bar using
// the charmbracelet/bubbles progress component for gradient rendering.
// Responds to StepChangedMsg.
type ProgressView struct {
	last          string
	colorA        string
	colorB        string
	solidFill     string
	width         int
	showPercent   bool
	hideStepLabel bool
}

// NewProgressView creates a progress bar view.
func NewProgressView(opts ...ProgressViewOption) *ProgressView {
	r := &ProgressView{
		width: 40,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *ProgressView) buildBar() progress.Model {
	var opts []progress.Option
	opts = append(opts, progress.WithWidth(r.width))
	if !r.showPercent {
		opts = append(opts, progress.WithoutPercentage())
	}
	if r.solidFill != "" {
		opts = append(opts, progress.WithColors(lipgloss.Color(r.solidFill)))
	} else if r.colorA != "" && r.colorB != "" {
		opts = append(opts, progress.WithColors(lipgloss.Color(r.colorA), lipgloss.Color(r.colorB)))
	} else {
		opts = append(opts, progress.WithDefaultBlend())
	}
	return progress.New(opts...)
}

func (r *ProgressView) Update(msg any) (string, []any) {
	sc, ok := msg.(StepChangedMsg)
	if !ok {
		return r.last, nil
	}

	if sc.Total <= 1 {
		r.last = ""
		return r.last, nil
	}

	pct := float64(sc.Current) / float64(sc.Total)
	bar := r.buildBar()

	rendered := bar.ViewAs(pct)
	if !r.hideStepLabel {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		label := fmt.Sprintf("  Step %d of %d", sc.Current, sc.Total)
		rendered += dimStyle.Render(label)
	}

	r.last = fmt.Sprintf("\n%s\n", rendered)
	return r.last, nil
}

func (r *ProgressView) Subscribe() []reflect.Type {
	return nil
}
