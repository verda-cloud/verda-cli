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
	"slices"
	"strings"
	"testing"
)

func ownsVerda(key []string) bool {
	return len(key) >= 2 && key[0] == "providers" && key[1] == "verda" ||
		len(key) >= 2 && key[0] == "models" && strings.HasPrefix(key[1], "verda/")
}

const verdaBlock = "[providers.verda]\ntype = \"openai_legacy\"\n\n[models.\"verda/m\"]\nprovider = \"verda\"\n"

func TestReplaceTOMLTablesRoundTrip(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"empty":               "",
		"top-level":           "default_model = \"kimi\" # keep\n",
		"no trailing newline": "a = 1",
		"tables and comments": `# Kimi config
default_model = "kimi-k2"

[providers.moonshot]
type = "kimi"
api_key = "sk-1"

# models
[models.kimi-k2]
provider = "moonshot"
text = """
[providers.verda]
not a header
"""
`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			added, err := editTOMLFile("c.toml", []byte(in), ownsVerda, verdaBlock)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(added), verdaBlock) {
				t.Fatalf("block missing:\n%s", added)
			}
			again, err := editTOMLFile("c.toml", added, ownsVerda, verdaBlock)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(added) {
				t.Errorf("re-run not idempotent\n--- first\n%s\n--- second\n%s", added, again)
			}
			removed, err := editTOMLFile("c.toml", added, ownsVerda, "")
			if err != nil {
				t.Fatal(err)
			}
			want := in
			if name == "no trailing newline" {
				want = in + "\n"
			}
			if string(removed) != want {
				t.Errorf("remove did not restore\n--- want\n%q\n--- got\n%q", want, removed)
			}
		})
	}
}

func TestReplaceTOMLTablesInPlaceKeepsFollowingTables(t *testing.T) {
	t.Parallel()
	in := "a = 1\n\n[providers.verda]\ntype = \"old\"\n\n# next\n[other]\nx = 1\n"
	out := replaceTOMLTables(in, ownsVerda, "[providers.verda]\ntype = \"new\"\n")
	want := "a = 1\n\n[providers.verda]\ntype = \"new\"\n\n# next\n[other]\nx = 1\n"
	if out != want {
		t.Errorf("got\n%q\nwant\n%q", out, want)
	}
	removed := replaceTOMLTables(out, ownsVerda, "")
	if removed != "a = 1\n\n# next\n[other]\nx = 1\n" {
		t.Errorf("removal: %q", removed)
	}
}

func TestEditTOMLRejectsInlineDuplicate(t *testing.T) {
	t.Parallel()
	in := "providers = { verda = { type = \"x\" } }\n"
	if _, err := editTOMLFile("c.toml", []byte(in), ownsVerda, verdaBlock); err == nil {
		t.Fatal("expected refusal when the result would redefine providers.verda")
	}
	if _, err := editTOMLFile("c.toml", []byte("a = "), ownsVerda, verdaBlock); err == nil {
		t.Fatal("expected refusal for invalid input")
	}
}

func TestParseTOMLKey(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		`providers.verda`:        {"providers", "verda"},
		`models."verda/qwen3.5"`: {"models", "verda/qwen3.5"},
		` models . 'verda/x' `:   {"models", "verda/x"},
		`"a\"b".c`:               {`a"b`, "c"},
	}
	for in, want := range cases {
		got, ok := parseTOMLKey(in)
		if !ok || !slices.Equal(got, want) {
			t.Errorf("parseTOMLKey(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"1, 2", "", "a.", `"open`} {
		if _, ok := parseTOMLKey(bad); ok {
			t.Errorf("parseTOMLKey(%q) accepted", bad)
		}
	}
	if got := tomlKey("models", "verda/x"); got != `models."verda/x"` {
		t.Errorf("tomlKey = %s", got)
	}
}
