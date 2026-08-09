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

import "strings"

// visibleWindow computes the visible slice bounds [start, end) for a
// scrolling viewport given total entries, cursor position, and page size.
func visibleWindow(total, cursor, pageSize int) (int, int) {
	if total == 0 {
		return 0, 0
	}
	if pageSize >= total {
		return 0, total
	}
	half := pageSize / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if end > total {
		end = total
		start = end - pageSize
	}
	return start, end
}

// refilter rebuilds matched from choices using filter. With a non-empty
// filter the passed-in slice is truncated and reused to avoid allocation;
// with an empty filter a fresh full-length slice is allocated (every
// index matches, so there is nothing to reuse). Returns the new matched
// slice; caller must reset cursor to 0.
func refilter(filter string, choices []string, matched []int) []int {
	if filter == "" {
		matched = make([]int, len(choices))
		for i := range choices {
			matched[i] = i
		}
	} else {
		lower := strings.ToLower(filter)
		matched = matched[:0]
		for i, c := range choices {
			if strings.Contains(strings.ToLower(c), lower) {
				matched = append(matched, i)
			}
		}
	}
	return matched
}
