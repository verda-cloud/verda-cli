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

// Package log provides a structured, leveled logging library for the
// verda-cli project, backed by go.uber.org/zap.
//
// It exposes both printf-style (*f) and structured key-value (*w) methods at
// every severity level (Debug, Info, Warn, Error, Panic, Fatal), a global
// logger with package-level convenience functions, context-aware field
// extraction via W(), and CLI flag integration through pflag.
//
// Quick start:
//
//	import "github.com/verda-cloud/verda-cli/pkg/log"
//
//	// Use package-level functions with the default global logger:
//	log.Infow("server starting", "port", 8080)
//	log.Infof("listening on :%d", 8080)
//
//	// Initialize with custom options (typically in main):
//	opts := log.NewOptions()
//	opts.Level = "debug"
//	log.Init(opts)
//	defer log.Sync()
//
//	// Pass the Logger interface for dependency injection:
//	svc := NewService(log.Default())
package log // import "github.com/verda-cloud/verda-cli/pkg/log"
