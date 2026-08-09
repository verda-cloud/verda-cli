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
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger defines the logging interface for the verda-cli project.
// It provides both printf-style (*f) and structured (*w) logging methods
// at all standard severity levels.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Panicf(format string, args ...any)
	Fatalf(format string, args ...any)

	Debugw(msg string, keysAndValues ...any)
	Infow(msg string, keysAndValues ...any)
	Warnw(msg string, keysAndValues ...any)
	Errorw(msg string, keysAndValues ...any)
	Panicw(msg string, keysAndValues ...any)
	Fatalw(msg string, keysAndValues ...any)

	// W returns a new Logger enriched with fields extracted from the context
	// (e.g., request ID, trace ID) via registered ContextExtractors.
	W(ctx context.Context) Logger

	// With returns a child Logger that always includes the given key-value pairs.
	With(keysAndValues ...any) Logger

	// AddCallerSkip returns a shallow clone with increased caller-skip depth,
	// useful when wrapping the Logger in higher-level helpers.
	AddCallerSkip(skip int) Logger

	// Sync flushes any buffered log entries.
	Sync()
}

// Field is an alias for zapcore.Field, exposed for callers that need
// to construct typed fields for advanced use cases.
type Field = zapcore.Field

// Option is a function that configures a zapLogger after construction.
type Option func(*zapLogger)

type zapLogger struct {
	z                 *zap.Logger
	opts              *Options
	contextExtractors map[string]func(context.Context) string
}

var _ Logger = (*zapLogger)(nil)

var (
	mu  sync.Mutex
	std = NewLogger(NewOptions())
)

// Init replaces the global Logger with one built from opts and options.
func Init(opts *Options, options ...Option) {
	mu.Lock()
	defer mu.Unlock()
	std = NewLogger(opts, options...)
}

// NewLogger creates a new Logger backed by zap.
// If opts is nil, default options are used.
func NewLogger(opts *Options, options ...Option) *zapLogger {
	if opts == nil {
		opts = NewOptions()
	}

	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(opts.Level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.MessageKey = "message"
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
	}
	encoderConfig.EncodeDuration = func(d time.Duration, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendFloat64(float64(d) / float64(time.Millisecond))
	}
	if opts.Format == "console" && opts.EnableColor {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	outputPaths := opts.OutputPaths
	if len(outputPaths) == 0 {
		outputPaths = []string{"stdout"}
	}

	cfg := &zap.Config{
		DisableCaller:     opts.DisableCaller,
		DisableStacktrace: opts.DisableStacktrace,
		Level:             zap.NewAtomicLevelAt(zapLevel),
		Encoding:          opts.Format,
		EncoderConfig:     encoderConfig,
		OutputPaths:       outputPaths,
		ErrorOutputPaths:  []string{"stderr"},
	}

	z, err := cfg.Build(zap.AddStacktrace(zapcore.PanicLevel), zap.AddCallerSkip(2))
	if err != nil {
		panic(err)
	}

	logger := &zapLogger{
		z:                 z,
		opts:              opts,
		contextExtractors: make(map[string]func(context.Context) string),
	}
	for _, opt := range options {
		opt(logger)
	}

	return logger
}

// Default returns the global Logger.
func Default() Logger { return std }

// Sync flushes the global Logger.
func Sync() { std.Sync() }

func (l *zapLogger) Sync() { _ = l.z.Sync() }

// Options returns the Options the logger was constructed with.
func (l *zapLogger) Options() *Options { return l.opts }

// --- Package-level convenience functions (delegate to global logger) ---

func Debugf(format string, args ...any)       { std.Debugf(format, args...) }
func Debugw(msg string, keysAndValues ...any) { std.Debugw(msg, keysAndValues...) }
func Infof(format string, args ...any)        { std.Infof(format, args...) }
func Infow(msg string, keysAndValues ...any)  { std.Infow(msg, keysAndValues...) }
func Warnf(format string, args ...any)        { std.Warnf(format, args...) }
func Warnw(msg string, keysAndValues ...any)  { std.Warnw(msg, keysAndValues...) }
func Errorf(format string, args ...any)       { std.Errorf(format, args...) }
func Errorw(msg string, keysAndValues ...any) { std.Errorw(msg, keysAndValues...) }
func Panicf(format string, args ...any)       { std.Panicf(format, args...) }
func Panicw(msg string, keysAndValues ...any) { std.Panicw(msg, keysAndValues...) }
func Fatalf(format string, args ...any)       { std.Fatalf(format, args...) }
func Fatalw(msg string, keysAndValues ...any) { std.Fatalw(msg, keysAndValues...) }
func W(ctx context.Context) Logger            { return std.W(ctx) }
func With(keysAndValues ...any) Logger        { return std.With(keysAndValues...) }
func AddCallerSkip(skip int) Logger           { return std.AddCallerSkip(skip) }

// --- zapLogger method implementations ---

func (l *zapLogger) Debugf(format string, args ...any) { l.logf(zapcore.DebugLevel, format, args...) }
func (l *zapLogger) Infof(format string, args ...any)  { l.logf(zapcore.InfoLevel, format, args...) }
func (l *zapLogger) Warnf(format string, args ...any)  { l.logf(zapcore.WarnLevel, format, args...) }
func (l *zapLogger) Errorf(format string, args ...any) { l.logf(zapcore.ErrorLevel, format, args...) }
func (l *zapLogger) Panicf(format string, args ...any) { l.logf(zapcore.PanicLevel, format, args...) }
func (l *zapLogger) Fatalf(format string, args ...any) { l.logf(zapcore.FatalLevel, format, args...) }

func (l *zapLogger) Debugw(msg string, keysAndValues ...any) {
	l.logw(zapcore.DebugLevel, msg, keysAndValues...)
}
func (l *zapLogger) Infow(msg string, keysAndValues ...any) {
	l.logw(zapcore.InfoLevel, msg, keysAndValues...)
}
func (l *zapLogger) Warnw(msg string, keysAndValues ...any) {
	l.logw(zapcore.WarnLevel, msg, keysAndValues...)
}
func (l *zapLogger) Errorw(msg string, keysAndValues ...any) {
	l.logw(zapcore.ErrorLevel, msg, keysAndValues...)
}
func (l *zapLogger) Panicw(msg string, keysAndValues ...any) {
	l.logw(zapcore.PanicLevel, msg, keysAndValues...)
}
func (l *zapLogger) Fatalw(msg string, keysAndValues ...any) {
	l.logw(zapcore.FatalLevel, msg, keysAndValues...)
}

func (l *zapLogger) With(keysAndValues ...any) Logger {
	lc := l.clone()
	lc.z = lc.z.Sugar().With(keysAndValues...).Desugar()
	return lc
}

func (l *zapLogger) AddCallerSkip(skip int) Logger {
	lc := l.clone()
	lc.z = lc.z.WithOptions(zap.AddCallerSkip(skip))
	return lc
}

func (l *zapLogger) clone() *zapLogger {
	copied := *l
	return &copied
}

// logf dispatches printf-style log calls through zap's SugaredLogger.*f methods.
func (l *zapLogger) logf(level zapcore.Level, format string, args ...any) {
	switch level {
	case zapcore.DebugLevel:
		l.z.Sugar().Debugf(format, args...)
	case zapcore.InfoLevel:
		l.z.Sugar().Infof(format, args...)
	case zapcore.WarnLevel:
		l.z.Sugar().Warnf(format, args...)
	case zapcore.ErrorLevel:
		l.z.Sugar().Errorf(format, args...)
	case zapcore.PanicLevel:
		l.z.Sugar().Panicf(format, args...)
	case zapcore.FatalLevel:
		l.z.Sugar().Fatalf(format, args...)
	}
}

// logw dispatches structured log calls through zap's SugaredLogger.*w methods.
func (l *zapLogger) logw(level zapcore.Level, msg string, keysAndValues ...any) {
	switch level {
	case zapcore.DebugLevel:
		l.z.Sugar().Debugw(msg, keysAndValues...)
	case zapcore.InfoLevel:
		l.z.Sugar().Infow(msg, keysAndValues...)
	case zapcore.WarnLevel:
		l.z.Sugar().Warnw(msg, keysAndValues...)
	case zapcore.ErrorLevel:
		l.z.Sugar().Errorw(msg, keysAndValues...)
	case zapcore.PanicLevel:
		l.z.Sugar().Panicw(msg, keysAndValues...)
	case zapcore.FatalLevel:
		l.z.Sugar().Fatalw(msg, keysAndValues...)
	}
}
