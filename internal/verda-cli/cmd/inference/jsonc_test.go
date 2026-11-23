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
	"strings"
	"testing"
)

func roundTripJSONC(t *testing.T, in string, path []string, value any) (added, removed string) {
	t.Helper()
	v, err := parseJSONC("test.json", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if err := setJSONCPath(v, path, value); err != nil {
		t.Fatal(err)
	}
	added = string(v.Pack())
	v2, err := parseJSONC("test.json", []byte(added))
	if err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, added)
	}
	if !removeJSONCPath(v2, path) {
		t.Fatalf("path %v not found after set:\n%s", path, added)
	}
	return added, string(v2.Pack())
}

func TestJSONCAddRemoveRestoresBytes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"comments and trailing commas": `{
  // my config
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude", // keep
  "provider": {
    "ollama": { "npm": "x" }, // trailing
  },
}
`,
		"no trailing commas": `{
  "model": "anthropic/claude",
  "provider": {
    "ollama": {"npm": "x"}
  }
}
`,
		"no parent object": "{\n\t\"model\": \"a/b\" /* c */\n}\n",
		"empty object":     "{}\n",
		"one line":         `{"model": "a/b"}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			added, removed := roundTripJSONC(t, in, []string{"provider", "verda"}, map[string]any{"npm": "@ai-sdk/openai-compatible"})
			if !strings.Contains(added, `"@ai-sdk/openai-compatible"`) {
				t.Fatalf("value not added:\n%s", added)
			}
			if removed != in {
				t.Errorf("remove did not restore the original\n--- want\n%s\n--- got\n%s", in, removed)
			}
		})
	}
}

func TestJSONCAddKeepsCommentsAndIndent(t *testing.T) {
	t.Parallel()
	in := "{\n    // keep me\n    \"provider\": {\n        \"ollama\": {}\n    }\n}\n"
	added, _ := roundTripJSONC(t, in, []string{"provider", "verda"}, map[string]any{"a": 1})
	want := "{\n    // keep me\n    \"provider\": {\n        \"ollama\": {},\n        \"verda\": {\n            \"a\": 1\n        }\n    }\n}\n"
	if added != want {
		t.Errorf("got\n%s\nwant\n%s", added, want)
	}
}

func TestJSONCSetReplacesExisting(t *testing.T) {
	t.Parallel()
	in := "{\n  \"provider\": {\n    \"verda\": {\"old\": true}, // ours\n    \"x\": {}\n  }\n}\n"
	v, err := parseJSONC("t", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if err := setJSONCPath(v, []string{"provider", "verda"}, map[string]any{"new": true}); err != nil {
		t.Fatal(err)
	}
	got := string(v.Pack())
	if strings.Contains(got, "old") || !strings.Contains(got, "// ours") || !strings.Contains(got, `"x": {}`) {
		t.Errorf("unexpected result:\n%s", got)
	}
}

func TestJSONCRejectsNonObject(t *testing.T) {
	t.Parallel()
	if _, err := parseJSONC("t", []byte(`[1]`)); err == nil {
		t.Fatal("expected error for array document")
	}
	if _, err := parseJSONC("t", []byte(`{"a": `)); err == nil {
		t.Fatal("expected error for broken document")
	}
}
