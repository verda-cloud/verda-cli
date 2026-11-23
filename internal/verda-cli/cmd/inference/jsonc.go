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
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

// JSONC edits work on the hujson AST so comments, trailing commas and the
// user's layout survive; hujson's own Patch/Format would reflow the file.

const defaultJSONIndent = "  "

func parseJSONC(path string, data []byte) (*hujson.Value, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}\n")
	}
	v, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON/JSONC: %w", path, err)
	}
	if _, ok := v.Value.(*hujson.Object); !ok {
		return nil, fmt.Errorf("%s: top-level value is not an object", path)
	}
	return &v, nil
}

func memberName(m *hujson.ObjectMember) string {
	if lit, ok := m.Name.Value.(hujson.Literal); ok {
		return lit.String()
	}
	return ""
}

func findMember(obj *hujson.Object, name string) int {
	for i := range obj.Members {
		if memberName(&obj.Members[i]) == name {
			return i
		}
	}
	return -1
}

// lineIndent is the whitespace after the last newline of extra, if extra ends
// in pure indentation.
func lineIndent(extra hujson.Extra) (string, bool) {
	s := string(extra)
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return "", false
	}
	tail := s[i+1:]
	if strings.Trim(tail, " \t") != "" {
		return "", false
	}
	return tail, true
}

func membersIndent(obj *hujson.Object, fallback string) string {
	for i := range obj.Members {
		if ind, ok := lineIndent(obj.Members[i].Name.BeforeExtra); ok {
			return ind
		}
	}
	return fallback
}

// indentUnit guesses the file's indent step from the top-level members.
func indentUnit(root *hujson.Value) string {
	obj := root.Value.(*hujson.Object)
	if ind := membersIndent(obj, ""); ind != "" {
		return ind
	}
	return defaultJSONIndent
}

func renderJSONValue(v any, indent, unit string) (hujson.ValueTrimmed, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(indent, unit)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	parsed, err := hujson.Parse(bytes.TrimSpace(buf.Bytes()))
	if err != nil {
		return nil, err
	}
	return parsed.Value, nil
}

func splitAtLastNewline(extra hujson.Extra) (head, tail string) {
	s := string(extra)
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i:]
}

func appendMember(obj *hujson.Object, name string, val hujson.ValueTrimmed, indent, closeIndent string) {
	head, tail := splitAtLastNewline(obj.AfterExtra)
	m := hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: hujson.Extra(head + "\n" + indent), Value: hujson.String(name)},
		Value: hujson.Value{BeforeExtra: hujson.Extra(" "), Value: val},
	}
	if n := len(obj.Members); n > 0 {
		last := &obj.Members[n-1]
		if last.Value.AfterExtra != nil {
			m.Value.AfterExtra = hujson.Extra{}
		} else {
			last.Value.AfterExtra = hujson.Extra{}
		}
	} else if tail == "" {
		tail = "\n" + closeIndent
	}
	obj.Members = append(obj.Members, m)
	obj.AfterExtra = hujson.Extra(tail)
}

// removeMember undoes appendMember byte-for-byte when the member is still last.
func removeMember(obj *hujson.Object, i int) {
	m := obj.Members[i]
	n := len(obj.Members)
	if i == n-1 {
		if n > 1 {
			prev := &obj.Members[i-1]
			switch {
			case m.Value.AfterExtra == nil && len(prev.Value.AfterExtra) == 0:
				prev.Value.AfterExtra = nil
			case m.Value.AfterExtra != nil && prev.Value.AfterExtra == nil:
				prev.Value.AfterExtra = hujson.Extra{}
			}
		}
		head, _ := splitAtLastNewline(m.Name.BeforeExtra)
		after := string(obj.AfterExtra)
		if n == 1 && head == "" && strings.TrimSpace(after) == "" {
			after = ""
		}
		obj.AfterExtra = hujson.Extra(head + after)
	}
	obj.Members = append(obj.Members[:i], obj.Members[i+1:]...)
}

// setJSONCPath sets root[path...] = value, creating missing parent objects.
func setJSONCPath(root *hujson.Value, path []string, value any) error {
	unit := indentUnit(root)
	obj := root.Value.(*hujson.Object)
	indent := ""
	for depth, seg := range path {
		childIndent := membersIndent(obj, indent+unit)
		i := findMember(obj, seg)
		last := depth == len(path)-1
		if i < 0 || last {
			v := value
			for j := len(path) - 1; j > depth; j-- {
				v = map[string]any{path[j]: v}
			}
			rendered, err := renderJSONValue(v, childIndent, unit)
			if err != nil {
				return err
			}
			if i >= 0 {
				obj.Members[i].Value.Value = rendered
			} else {
				appendMember(obj, seg, rendered, childIndent, indent)
			}
			return nil
		}
		next, ok := obj.Members[i].Value.Value.(*hujson.Object)
		if !ok {
			return fmt.Errorf("%q is not an object", strings.Join(path[:depth+1], "."))
		}
		obj, indent = next, childIndent
	}
	return nil
}

// removeJSONCPath deletes root[path...] and drops a parent object that only
// existed to hold it (empty and laid out the way appendMember creates it).
func removeJSONCPath(root *hujson.Value, path []string) bool {
	objs := []*hujson.Object{root.Value.(*hujson.Object)}
	for _, seg := range path[:len(path)-1] {
		cur := objs[len(objs)-1]
		i := findMember(cur, seg)
		if i < 0 {
			return false
		}
		next, ok := cur.Members[i].Value.Value.(*hujson.Object)
		if !ok {
			return false
		}
		objs = append(objs, next)
	}
	leaf := objs[len(objs)-1]
	i := findMember(leaf, path[len(path)-1])
	if i < 0 {
		return false
	}
	removeMember(leaf, i)
	for d := len(objs) - 1; d > 0; d-- {
		if len(objs[d].Members) > 0 || len(objs[d].AfterExtra) > 0 {
			break
		}
		parent := objs[d-1]
		if j := findMember(parent, path[d-1]); j >= 0 {
			removeMember(parent, j)
		}
	}
	return true
}

func jsoncHasPath(root *hujson.Value, path ...string) bool {
	obj := root.Value.(*hujson.Object)
	for d, seg := range path {
		i := findMember(obj, seg)
		if i < 0 {
			return false
		}
		if d == len(path)-1 {
			return true
		}
		next, ok := obj.Members[i].Value.Value.(*hujson.Object)
		if !ok {
			return false
		}
		obj = next
	}
	return false
}
