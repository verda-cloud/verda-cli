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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// TOML edits are text-level: our tables are cut out and re-inserted so every
// byte outside them survives. go-toml only validates the result.

var tomlHeaderRe = regexp.MustCompile(`^\s*\[\[?\s*(.+?)\s*\]\]?\s*(#.*)?$`)

var tomlBareKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type tomlHeader struct {
	line int
	key  []string
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04X", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlKeySegment(s string) string {
	if tomlBareKeyRe.MatchString(s) {
		return s
	}
	return tomlString(s)
}

func tomlKey(segments ...string) string {
	parts := make([]string, len(segments))
	for i, s := range segments {
		parts[i] = tomlKeySegment(s)
	}
	return strings.Join(parts, ".")
}

// parseTOMLKey parses a dotted key such as models."verda/x"; ok=false when s is
// not a key (e.g. an array literal line inside a multi-line value).
func parseTOMLKey(s string) (segments []string, ok bool) {
	for {
		seg, rest, ok := parseTOMLKeySegment(strings.TrimLeft(s, " \t"))
		if !ok {
			return nil, false
		}
		segments = append(segments, seg)
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return segments, true
		}
		if rest[0] != '.' {
			return nil, false
		}
		s = rest[1:]
	}
}

func isBareKeyByte(c byte) bool {
	return c == '_' || c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func parseTOMLKeySegment(s string) (seg, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	switch s[0] {
	case '"':
		end := 1
		for end < len(s) && s[end] != '"' {
			if s[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(s) {
			return "", "", false
		}
		unq, err := strconv.Unquote(s[:end+1])
		if err != nil {
			return "", "", false
		}
		return unq, s[end+1:], true
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", "", false
		}
		return s[1 : end+1], s[end+2:], true
	}
	end := 0
	for end < len(s) && isBareKeyByte(s[end]) {
		end++
	}
	if end == 0 {
		return "", "", false
	}
	return s[:end], s[end:], true
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func isBlankLine(l string) bool { return strings.TrimSpace(l) == "" }

func isBlankOrCommentLine(l string) bool {
	t := strings.TrimSpace(l)
	return t == "" || strings.HasPrefix(t, "#")
}

// scanTOMLHeaders skips lines inside multi-line strings, where "[x]" is data.
func scanTOMLHeaders(lines []string) []tomlHeader {
	var headers []tomlHeader
	inBasic, inLiteral := false, false
	for i, l := range lines {
		if !inBasic && !inLiteral {
			if m := tomlHeaderRe.FindStringSubmatch(strings.TrimRight(l, "\r\n")); m != nil {
				if key, ok := parseTOMLKey(m[1]); ok {
					headers = append(headers, tomlHeader{line: i, key: key})
					continue
				}
			}
		}
		if !inLiteral && strings.Count(l, `"""`)%2 == 1 {
			inBasic = !inBasic
		}
		if !inBasic && strings.Count(l, `'''`)%2 == 1 {
			inLiteral = !inLiteral
		}
	}
	return headers
}

// replaceTOMLTables cuts every table whose header owned() claims (plus the
// blank line before it) and inserts block where the first one was, else at the
// end. An empty block only removes. Re-running with the same block is a no-op.
func replaceTOMLTables(src string, owned func(key []string) bool, block string) string {
	lines := splitLines(src)
	headers := scanTOMLHeaders(lines)
	remove := make([]bool, len(lines))
	insertAt := -1
	for i, h := range headers {
		if !owned(h.key) {
			continue
		}
		end := len(lines)
		if i+1 < len(headers) {
			end = headers[i+1].line
		}
		for end > h.line+1 && isBlankOrCommentLine(lines[end-1]) {
			end--
		}
		start := h.line
		for j := start; j < end; j++ {
			remove[j] = true
		}
		if start > 0 && !remove[start-1] && isBlankLine(lines[start-1]) {
			start--
			remove[start] = true
		}
		if insertAt < 0 {
			insertAt = start
		}
	}

	out := make([]string, 0, len(lines)+1)
	newInsert := -1
	for i, l := range lines {
		if i == insertAt {
			newInsert = len(out)
		}
		if !remove[i] {
			out = append(out, l)
		}
	}
	if block == "" {
		return strings.Join(out, "")
	}
	if newInsert < 0 {
		newInsert = len(out)
	}
	ins := block
	if newInsert > 0 {
		if !strings.HasSuffix(out[newInsert-1], "\n") {
			out[newInsert-1] += "\n"
		}
		ins = "\n" + block
	}
	out = append(out[:newInsert], append([]string{ins}, out[newInsert:]...)...)
	return strings.Join(out, "")
}

func parseTOMLDoc(path string, data []byte) (map[string]any, error) {
	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	return doc, nil
}

func tomlTableAt(doc map[string]any, path ...string) (map[string]any, bool) {
	cur := doc
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// tomlKV renders `key = value` lines; values are string, int64 or []string.
type tomlKV struct {
	Key   string
	Value any
}

func renderTOMLTable(key string, kvs []tomlKV) string {
	return "[" + key + "]\n" + renderTOMLKVs(kvs)
}

func renderTOMLKVs(kvs []tomlKV) string {
	var b strings.Builder
	for _, kv := range kvs {
		b.WriteString(tomlKeySegment(kv.Key) + " = ")
		switch v := kv.Value.(type) {
		case string:
			b.WriteString(tomlString(v))
		case int64:
			b.WriteString(strconv.FormatInt(v, 10))
		default:
			b.WriteString(fmt.Sprint(v))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// editTOMLFile replaces our tables in data and validates both input and result,
// so a file we cannot parse is never rewritten.
func editTOMLFile(path string, data []byte, owned func([]string) bool, block string) ([]byte, error) {
	if _, err := parseTOMLDoc(path, data); err != nil {
		return nil, err
	}
	out := []byte(replaceTOMLTables(string(data), owned, block))
	if _, err := parseTOMLDoc(path, out); err != nil {
		return nil, fmt.Errorf("refusing to write %s: the edit would not parse (are verda entries defined inline?): %w", path, err)
	}
	return out, nil
}
