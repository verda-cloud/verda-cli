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

package version

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestGet_PopulatesRuntimeFields(t *testing.T) {
	info := Get()
	if info.GoVersion != runtime.Version() {
		t.Errorf("expected GoVersion %q, got %q", runtime.Version(), info.GoVersion)
	}
	if info.Compiler != runtime.Compiler {
		t.Errorf("expected Compiler %q, got %q", runtime.Compiler, info.Compiler)
	}
	if !strings.Contains(info.Platform, "/") {
		t.Errorf("expected Platform to contain '/', got %q", info.Platform)
	}
}

func TestGet_DefaultBuildVars(t *testing.T) {
	info := Get()
	if info.GitVersion == "" {
		t.Error("GitVersion should not be empty")
	}
}

func TestString(t *testing.T) {
	info := Get()
	if info.String() != info.GitVersion {
		t.Errorf("String() should return GitVersion, got %q", info.String())
	}
}

func TestToJSON_ValidJSON(t *testing.T) {
	info := Get()
	j := info.ToJSON()
	var parsed map[string]string
	if err := json.Unmarshal([]byte(j), &parsed); err != nil {
		t.Fatalf("ToJSON() produced invalid JSON: %v", err)
	}
	if parsed["goVersion"] != runtime.Version() {
		t.Errorf("JSON goVersion = %q, want %q", parsed["goVersion"], runtime.Version())
	}
}

func TestText_ContainsAllFields(t *testing.T) {
	info := Get()
	text := info.Text()
	for _, field := range []string{"gitVersion:", "gitCommit:", "gitTreeState:", "buildDate:", "goVersion:", "compiler:", "platform:"} {
		if !strings.Contains(text, field) {
			t.Errorf("Text() missing field %q", field)
		}
	}
}

func TestVersionValue_Set(t *testing.T) {
	tests := []struct {
		input    string
		expected versionValue
		wantErr  bool
	}{
		{"true", versionEnabled, false},
		{"false", versionNotSet, false},
		{"raw", versionRaw, false},
		{"invalid", 0, true},
	}

	for _, tc := range tests {
		var v versionValue
		err := v.Set(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("Set(%q): unexpected error state: %v", tc.input, err)
			continue
		}
		if err == nil && v != tc.expected {
			t.Errorf("Set(%q) = %v, want %v", tc.input, v, tc.expected)
		}
	}
}

func TestVersionValue_String(t *testing.T) {
	tests := []struct {
		val  versionValue
		want string
	}{
		{versionNotSet, "false"},
		{versionEnabled, "true"},
		{versionRaw, "raw"},
	}
	for _, tc := range tests {
		if got := tc.val.String(); got != tc.want {
			t.Errorf("(%d).String() = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestVersionValue_Type(t *testing.T) {
	var v versionValue
	if v.Type() != "version" {
		t.Errorf("Type() = %q, want 'version'", v.Type())
	}
}
