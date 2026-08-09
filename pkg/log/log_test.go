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

package log

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newBufferedLogger creates a zapLogger that writes JSON to buf at the given level.
// Caller skip is set to 0 so test call sites show correctly.
func newBufferedLogger(buf *bytes.Buffer, level zapcore.Level) *zapLogger {
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(buf), level)
	z := zap.New(core) // no caller skip for test clarity
	return &zapLogger{
		z:                 z,
		opts:              NewOptions(),
		contextExtractors: make(map[string]func(context.Context) string),
	}
}

// parseLine unmarshals one JSON log line into a map.
func parseLine(t *testing.T, line string) map[string]any {
	t.Helper()
	m := make(map[string]any)
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("failed to parse log line %q: %v", line, err)
	}
	return m
}

// lastLine returns the final non-empty line from the buffer.
func lastLine(buf *bytes.Buffer) string {
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	return lines[len(lines)-1]
}

func TestNewLogger_Defaults(t *testing.T) {
	logger := NewLogger(nil)
	if logger == nil {
		t.Fatal("NewLogger(nil) returned nil")
	}
	if logger.opts.Level != "info" {
		t.Errorf("expected default level 'info', got %q", logger.opts.Level)
	}
	if logger.opts.Format != "console" {
		t.Errorf("expected default format 'console', got %q", logger.opts.Format)
	}
}

func TestNewLogger_InvalidLevel(t *testing.T) {
	opts := &Options{Level: "not-a-level", Format: "json", OutputPaths: []string{"stdout"}}
	logger := NewLogger(opts)
	if logger == nil {
		t.Fatal("NewLogger returned nil for invalid level")
	}
}

func TestInfow_StructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	l.Infow("hello", "user", "alice", "count", 42)
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if m["msg"] != "hello" {
		t.Errorf("expected msg 'hello', got %v", m["msg"])
	}
	if m["user"] != "alice" {
		t.Errorf("expected user 'alice', got %v", m["user"])
	}
	if m["count"] != float64(42) {
		t.Errorf("expected count 42, got %v", m["count"])
	}
}

func TestInfof_PrintfFormatting(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	l.Infof("user %s has %d items", "bob", 5)
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	msg, _ := m["msg"].(string)
	if msg != "user bob has 5 items" {
		t.Errorf("expected formatted message, got %q", msg)
	}
}

func TestLogfVsLogw_DifferentBehavior(t *testing.T) {
	var bufF, bufW bytes.Buffer
	lf := newBufferedLogger(&bufF, zapcore.DebugLevel)
	lw := newBufferedLogger(&bufW, zapcore.DebugLevel)

	lf.Infof("count=%d", 10)
	lw.Infow("count", "value", 10)
	_ = lf.z.Sync()
	_ = lw.z.Sync()

	mf := parseLine(t, lastLine(&bufF))
	mw := parseLine(t, lastLine(&bufW))

	// Printf should produce a formatted message string
	if mf["msg"] != "count=10" {
		t.Errorf("Infof: expected msg 'count=10', got %q", mf["msg"])
	}
	// Structured should produce "count" as message and "value" as a field
	if mw["msg"] != "count" {
		t.Errorf("Infow: expected msg 'count', got %q", mw["msg"])
	}
	if mw["value"] != float64(10) {
		t.Errorf("Infow: expected value=10, got %v", mw["value"])
	}
}

func TestWith_ChildLogger(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	child := l.With("component", "auth")
	child.Infow("token issued", "user", "carol")
	child.(interface{ Sync() }).Sync()

	m := parseLine(t, lastLine(&buf))
	if m["component"] != "auth" {
		t.Errorf("expected component 'auth', got %v", m["component"])
	}
	if m["user"] != "carol" {
		t.Errorf("expected user 'carol', got %v", m["user"])
	}
}

func TestWith_DoesNotMutateParent(t *testing.T) {
	var buf bytes.Buffer
	parent := newBufferedLogger(&buf, zapcore.DebugLevel)

	_ = parent.With("child_field", "yes")
	parent.Infow("parent log")
	_ = parent.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if _, exists := m["child_field"]; exists {
		t.Error("With() should not mutate the parent logger")
	}
}

func TestW_ContextExtraction(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)
	l.contextExtractors["request.id"] = func(ctx context.Context) string {
		if v, ok := ctx.Value("rid").(string); ok {
			return v
		}
		return ""
	}

	ctx := context.WithValue(context.Background(), "rid", "abc-123") //nolint:staticcheck // test matches extractor key type
	l.W(ctx).Infow("handled")
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if m["request.id"] != "abc-123" {
		t.Errorf("expected request.id 'abc-123', got %v", m["request.id"])
	}
}

func TestW_EmptyContextValue_OmitsField(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)
	l.contextExtractors["trace.id"] = func(ctx context.Context) string { return "" }

	l.W(context.Background()).Infow("no trace")
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if _, exists := m["trace.id"]; exists {
		t.Error("W() should omit fields with empty extractor values")
	}
}

func TestAddCallerSkip(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	skipped := l.AddCallerSkip(5)
	if skipped == nil {
		t.Fatal("AddCallerSkip returned nil")
	}
	// Ensure it's a different instance
	if skipped.(*zapLogger) == l {
		t.Error("AddCallerSkip should return a new logger, not the same one")
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.WarnLevel)

	l.Infow("should be filtered")
	l.Debugf("also filtered")
	l.Warnw("should appear")
	_ = l.z.Sync()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line at warn level, got %d: %v", len(lines), lines)
	}
	m := parseLine(t, lines[0])
	if m["msg"] != "should appear" {
		t.Errorf("expected msg 'should appear', got %v", m["msg"])
	}
}

func TestAllLevels_Structured(t *testing.T) {
	tests := []struct {
		name  string
		logFn func(l *zapLogger)
		level string
	}{
		{"Debugw", func(l *zapLogger) { l.Debugw("d") }, "debug"},
		{"Infow", func(l *zapLogger) { l.Infow("i") }, "info"},
		{"Warnw", func(l *zapLogger) { l.Warnw("w") }, "warn"},
		{"Errorw", func(l *zapLogger) { l.Errorw("e") }, "error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := newBufferedLogger(&buf, zapcore.DebugLevel)
			tc.logFn(l)
			_ = l.z.Sync()

			if buf.Len() == 0 {
				t.Errorf("%s produced no output", tc.name)
			}
		})
	}
}

func TestAllLevels_Printf(t *testing.T) {
	tests := []struct {
		name  string
		logFn func(l *zapLogger)
	}{
		{"Debugf", func(l *zapLogger) { l.Debugf("val=%d", 1) }},
		{"Infof", func(l *zapLogger) { l.Infof("val=%d", 2) }},
		{"Warnf", func(l *zapLogger) { l.Warnf("val=%d", 3) }},
		{"Errorf", func(l *zapLogger) { l.Errorf("val=%d", 4) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := newBufferedLogger(&buf, zapcore.DebugLevel)
			tc.logFn(l)
			_ = l.z.Sync()

			if buf.Len() == 0 {
				t.Errorf("%s produced no output", tc.name)
			}
		})
	}
}

func TestInit_ReplacesGlobal(t *testing.T) {
	oldStd := std
	defer func() {
		mu.Lock()
		std = oldStd
		mu.Unlock()
	}()

	opts := &Options{Level: "debug", Format: "json", OutputPaths: []string{"stdout"}}
	Init(opts)

	if Default() == nil {
		t.Fatal("Default() returned nil after Init")
	}
}

func TestInit_PassesOptions(t *testing.T) {
	oldStd := std
	defer func() {
		mu.Lock()
		std = oldStd
		mu.Unlock()
	}()

	called := false
	testOption := func(l *zapLogger) {
		called = true
	}

	opts := &Options{Level: "info", Format: "json", OutputPaths: []string{"stdout"}}
	Init(opts, testOption)

	if !called {
		t.Error("Init should pass functional options to NewLogger")
	}
}

func TestSync_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)
	l.Sync() // should not panic
}

func TestDefault_NotNil(t *testing.T) {
	if Default() == nil {
		t.Fatal("Default() should never return nil")
	}
}

func TestErrorw_ConsistentSignature(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	l.Errorw("db failed", "err", "connection refused", "host", "db.local")
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if m["msg"] != "db failed" {
		t.Errorf("expected msg 'db failed', got %v", m["msg"])
	}
	if m["err"] != "connection refused" {
		t.Errorf("expected err 'connection refused', got %v", m["err"])
	}
	if m["host"] != "db.local" {
		t.Errorf("expected host 'db.local', got %v", m["host"])
	}
}

func TestClone_Independence(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	c := l.clone()
	if c == l {
		t.Error("clone should return a different pointer")
	}
	// Mutating clone's opts should not affect original
	c.opts = &Options{Level: "error"}
	if l.opts.Level == "error" {
		t.Error("clone opts mutation leaked to original")
	}
}

func TestWithContextExtractor_Option(t *testing.T) {
	var buf bytes.Buffer
	l := newBufferedLogger(&buf, zapcore.DebugLevel)

	opt := WithContextExtractor(ContextExtractors{
		"tenant": func(ctx context.Context) string { return "acme" },
	})
	opt(l)

	ctx := context.Background()
	l.W(ctx).Infow("with tenant")
	_ = l.z.Sync()

	m := parseLine(t, lastLine(&buf))
	if m["tenant"] != "acme" {
		t.Errorf("expected tenant 'acme', got %v", m["tenant"])
	}
}
