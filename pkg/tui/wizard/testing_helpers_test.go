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

// testResultCh creates a buffered channel pre-filled with promptResults.
// The channel is closed after all results are written, so subsequent reads
// return the zero value (ActionExit) instead of blocking forever.
func testResultCh(results ...promptResult) chan promptResult {
	ch := make(chan promptResult, len(results))
	for _, r := range results {
		ch <- r
	}
	close(ch)
	return ch
}

// selectResult creates a promptResult for selecting an index.
func selectResult(idx int) promptResult {
	return promptResult{value: idx, action: ActionNone}
}

// textResult creates a promptResult for text input.
func textResult(text string) promptResult {
	return promptResult{value: text, action: ActionNone}
}

// confirmResult creates a promptResult for confirm.
func confirmResult(yes bool) promptResult {
	return promptResult{value: yes, action: ActionNone}
}

// multiSelectResult creates a promptResult for multi-select.
func multiSelectResult(indices []int) promptResult {
	return promptResult{value: indices, action: ActionNone}
}

// passwordResult creates a promptResult for password input.
func passwordResult(text string) promptResult {
	return promptResult{value: text, action: ActionNone}
}

// backResult creates a promptResult for back navigation (Esc).
func backResult() promptResult {
	return promptResult{action: ActionBack}
}

// exitResult creates a promptResult for exit (Ctrl+C).
func exitResult() promptResult {
	return promptResult{action: ActionExit}
}
