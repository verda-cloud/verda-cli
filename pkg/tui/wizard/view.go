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

import "reflect"

// View is an actor that receives messages and renders output.
// Each view maintains its own state and renders independently.
type View interface {
	// Update receives a message and returns:
	// - render: the new display string for this view
	// - publish: optional messages to broadcast to other views
	Update(msg any) (render string, publish []any)

	// Subscribe returns the message types this view listens to.
	// nil means receive all engine broadcasts only.
	// Non-nil means receive only those types (plus engine broadcasts).
	Subscribe() []reflect.Type
}

// ViewDef defines a view in the layout.
type ViewDef struct {
	ID   string
	View View
}

// --- Engine broadcast messages ---

// StepChangedMsg is broadcast when the engine moves to a new step.
type StepChangedMsg struct {
	Current    int
	Total      int
	StepName   string
	PromptType PromptType
	Collected  map[string]any
}

// CollectedChangedMsg is broadcast when a step completes and collected values change.
type CollectedChangedMsg struct {
	Key       string
	Value     any
	Collected map[string]any
}

// StoreChangedMsg is broadcast when a value in the store is set.
type StoreChangedMsg struct {
	Key   string
	Value any
}
