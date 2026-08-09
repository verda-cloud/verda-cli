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

import "time"

// --- Spinner Options ---

// SpinnerStyle defines the visual style of a spinner.
type SpinnerStyle int

const (
	SpinnerDot SpinnerStyle = iota
	SpinnerLine
	SpinnerMiniDot
	SpinnerJump
	SpinnerPulse
	SpinnerPoints
	SpinnerGlobe
	SpinnerMoon
	SpinnerMeter
	SpinnerEllipsis
)

// SpinnerOption configures a Spinner.
type SpinnerOption func(*SpinnerConfig)

// SpinnerConfig holds resolved Spinner settings.
type SpinnerConfig struct {
	Style       SpinnerStyle
	DoneSymbol  string // symbol shown when stopped (default: "✓")
	ErrorSymbol string // symbol shown on error (default: "✗")
}

// WithSpinnerStyle sets the spinner animation style.
func WithSpinnerStyle(s SpinnerStyle) SpinnerOption {
	return func(c *SpinnerConfig) { c.Style = s }
}

// WithDoneSymbol sets the symbol shown when the spinner completes.
func WithDoneSymbol(s string) SpinnerOption {
	return func(c *SpinnerConfig) { c.DoneSymbol = s }
}

// WithErrorSymbol sets the symbol shown when the spinner stops with error.
func WithErrorSymbol(s string) SpinnerOption {
	return func(c *SpinnerConfig) { c.ErrorSymbol = s }
}

// ResolveSpinnerConfig applies options to a default SpinnerConfig.
func ResolveSpinnerConfig(opts []SpinnerOption) SpinnerConfig {
	cfg := SpinnerConfig{
		Style:       SpinnerDot,
		DoneSymbol:  "✓",
		ErrorSymbol: "✗",
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// --- Progress Options ---

// ProgressOption configures a Progress bar.
type ProgressOption func(*ProgressConfig)

// ProgressConfig holds resolved Progress settings.
type ProgressConfig struct {
	Width        int           // bar width in characters (default: 40)
	ShowPercent  bool          // show percentage text (default: true)
	ColorA       string        // gradient start color (default: "#5A56E0")
	ColorB       string        // gradient end color (default: "#EE6FF8")
	SolidFill    string        // if set, uses solid fill instead of gradient
	AutoStop     bool          // auto-stop when reaching 100% (default: true)
	PollInterval time.Duration // for non-animated backends (default: 100ms)
}

// WithProgressWidth sets the width of the progress bar.
func WithProgressWidth(w int) ProgressOption {
	return func(c *ProgressConfig) { c.Width = w }
}

// WithProgressGradient sets gradient colors for the progress bar.
func WithProgressGradient(colorA, colorB string) ProgressOption {
	return func(c *ProgressConfig) {
		c.ColorA = colorA
		c.ColorB = colorB
		c.SolidFill = ""
	}
}

// WithProgressSolidFill uses a solid color for the progress bar.
func WithProgressSolidFill(color string) ProgressOption {
	return func(c *ProgressConfig) {
		c.SolidFill = color
		c.ColorA = ""
		c.ColorB = ""
	}
}

// WithoutPercent hides the percentage text.
func WithoutPercent() ProgressOption {
	return func(c *ProgressConfig) { c.ShowPercent = false }
}

// ResolveProgressConfig applies options to a default ProgressConfig.
func ResolveProgressConfig(opts []ProgressOption) ProgressConfig {
	cfg := ProgressConfig{
		Width:        40,
		ShowPercent:  true,
		ColorA:       "#5A56E0",
		ColorB:       "#EE6FF8",
		AutoStop:     true,
		PollInterval: 100 * time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// --- Table Options ---

// TableOption configures a Table render.
type TableOption func(*TableConfig)

// TableConfig holds resolved Table settings.
type TableConfig struct {
	MaxWidth int // max table width (0 = no limit)
}

// WithTableMaxWidth sets the maximum table width.
func WithTableMaxWidth(w int) TableOption {
	return func(c *TableConfig) { c.MaxWidth = w }
}

// ResolveTableConfig applies options to a default TableConfig.
func ResolveTableConfig(opts []TableOption) TableConfig {
	cfg := TableConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// --- Pager Options ---

// PagerOption configures a Pager.
type PagerOption func(*PagerConfig)

// PagerConfig holds resolved Pager settings.
type PagerConfig struct {
	Title       string // optional title shown in the header
	LineNumbers bool   // show line numbers
}

// WithPagerTitle sets a title displayed in the pager header.
func WithPagerTitle(t string) PagerOption {
	return func(c *PagerConfig) { c.Title = t }
}

// WithLineNumbers enables line numbers in the pager.
func WithLineNumbers() PagerOption {
	return func(c *PagerConfig) { c.LineNumbers = true }
}

// ResolvePagerConfig applies options to a default PagerConfig.
func ResolvePagerConfig(opts []PagerOption) PagerConfig {
	cfg := PagerConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}
