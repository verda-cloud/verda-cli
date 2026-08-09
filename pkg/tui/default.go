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

import "sync"

var (
	mu            sync.Mutex
	builder       func(ioOpts ...func(*IO)) Prompter
	statusBuilder func(ioOpts ...func(*IO)) Status
)

// RegisterBuilder sets the factory used by Default(). Called by backend packages in init().
// This enables swapping backends without import-path changes in calling code.
func RegisterBuilder(fn func(ioOpts ...func(*IO)) Prompter) {
	mu.Lock()
	defer mu.Unlock()
	builder = fn
}

// Default returns a Prompter created by the registered builder.
// Panics if no builder has been registered — import a backend package
// (e.g., _ "github.com/verda-cloud/verda-cli/pkg/tui/bubbletea") to register one.
func Default(ioOpts ...func(*IO)) Prompter {
	mu.Lock()
	defer mu.Unlock()
	if builder == nil {
		panic("tui: no backend registered — import a backend package")
	}
	return builder(ioOpts...)
}

// RegisterStatusBuilder sets the factory used by DefaultStatus().
func RegisterStatusBuilder(fn func(ioOpts ...func(*IO)) Status) {
	mu.Lock()
	defer mu.Unlock()
	statusBuilder = fn
}

// DefaultStatus returns a Status created by the registered builder.
// Panics if no builder has been registered.
func DefaultStatus(ioOpts ...func(*IO)) Status {
	mu.Lock()
	defer mu.Unlock()
	if statusBuilder == nil {
		panic("tui: no status backend registered — import a backend package")
	}
	return statusBuilder(ioOpts...)
}
