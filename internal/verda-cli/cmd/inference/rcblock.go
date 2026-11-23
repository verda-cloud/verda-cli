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

package inference

import (
	"path/filepath"
	"strings"
)

const (
	rcBlockStart = "# >>> verda inference >>>"
	rcBlockEnd   = "# <<< verda inference <<<"
)

// rcFileFor picks the rc file for $SHELL; "" means unsupported (aliases are printed).
// bash: ~/.bashrc when present, else ~/.bash_profile on macOS (Terminal opens login shells).
func rcFileFor(env *hostEnv, exists func(string) bool) string {
	switch filepath.Base(env.Getenv("SHELL")) {
	case "zsh":
		dir := env.Getenv("ZDOTDIR")
		if dir == "" {
			dir = env.Home
		}
		return filepath.Join(dir, ".zshrc")
	case "bash":
		bashrc := filepath.Join(env.Home, ".bashrc")
		if exists(bashrc) || env.GOOS != "darwin" {
			return bashrc
		}
		return filepath.Join(env.Home, ".bash_profile")
	}
	return ""
}

func renderRCBlock(aliases []string) string {
	if len(aliases) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(rcBlockStart + "\n")
	b.WriteString("# Managed by `verda inference setup`; `verda inference remove` deletes it.\n")
	for _, a := range aliases {
		b.WriteString(a + "\n")
	}
	b.WriteString(rcBlockEnd + "\n")
	return b.String()
}

// replaceRCBlock swaps our marked block for block ("" removes it), keeping
// every other line; same placement rules as replaceTOMLTables.
func replaceRCBlock(src, block string) string {
	lines := splitLines(src)
	start, end := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == rcBlockStart && start < 0 {
			start = i
		}
		if t == rcBlockEnd && start >= 0 {
			end = i
			break
		}
	}
	insertAt := len(lines)
	if start >= 0 && end >= start {
		cut := start
		if cut > 0 && isBlankLine(lines[cut-1]) {
			cut--
		}
		lines = append(lines[:cut:cut], lines[end+1:]...)
		insertAt = cut
	}
	if block == "" {
		return strings.Join(lines, "")
	}
	ins := block
	if insertAt > 0 {
		if !strings.HasSuffix(lines[insertAt-1], "\n") {
			lines[insertAt-1] += "\n"
		}
		ins = "\n" + block
	}
	lines = append(lines[:insertAt:insertAt], append([]string{ins}, lines[insertAt:]...)...)
	return strings.Join(lines, "")
}
