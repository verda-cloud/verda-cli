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

package bubbletea

// Used by editor/pager/progress/spinner (msg.String() comparison).
// Prompt models now dispatch via KeyBinding and don't reference these.
const (
	keyCtrlC = "ctrl+c"
	keyEsc   = "esc"
)

// Shared across prompt models so the goconst linter stays quiet.
const (
	hintEscBack    = "esc back"
	hintCtrlCExit  = "ctrl+c exit"
	hintEnterEntry = "enter submit"
)
