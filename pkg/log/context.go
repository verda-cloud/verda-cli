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

	"go.uber.org/zap"
)

// ContextExtractors maps field names to functions that extract their values
// from a context.Context. Registered extractors are invoked by W() to enrich
// log entries with request-scoped metadata (e.g., request ID, trace ID).
type ContextExtractors map[string]func(context.Context) string

// WithContextExtractor returns an Option that registers the given extractors.
// Multiple calls are additive; later registrations for the same key overwrite
// earlier ones.
func WithContextExtractor(extractors ContextExtractors) Option {
	return func(l *zapLogger) {
		for k, v := range extractors {
			l.contextExtractors[k] = v
		}
	}
}

// W returns a new Logger whose output is enriched with fields extracted
// from ctx by the registered ContextExtractors.
func (l *zapLogger) W(ctx context.Context) Logger {
	lc := l.clone()
	for fieldName, extractor := range l.contextExtractors {
		if val := extractor(ctx); val != "" {
			lc.z = lc.z.With(zap.String(fieldName, val))
		}
	}
	return lc
}
