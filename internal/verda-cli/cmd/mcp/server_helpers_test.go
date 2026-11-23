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

package mcp

import (
	"strings"
	"testing"
)

func TestJSONTypeNameCoversJSONKinds(t *testing.T) {
	t.Parallel()
	cases := map[string]any{
		"a string":  "x",
		"a boolean": true,
		"a number":  float64(1),
		"an array":  []any{},
		"an object": map[string]any{},
	}
	for want, v := range cases {
		if got := jsonTypeName(v); got != want {
			t.Errorf("jsonTypeName(%T) = %q, want %q", v, got, want)
		}
	}
	if got := jsonTypeName(int(3)); got != "a number" {
		t.Errorf("int = %q", got)
	}
	if got := jsonTypeName(struct{}{}); !strings.Contains(got, "struct") {
		t.Errorf("unknown kinds fall back to the Go type: %q", got)
	}
}

func TestToolErrorResultWithoutAnError(t *testing.T) {
	t.Parallel()
	res := toolErrorResult(nil)
	if !res.IsError || resultText(t, res) != "unknown error" {
		t.Fatalf("nil error must still be an error result: %v", res)
	}
}

func TestJSONResultMarshalFailure(t *testing.T) {
	t.Parallel()
	if _, err := jsonResult(make(chan int)); err == nil {
		t.Fatal("an unmarshalable value must be reported as a Go error")
	}
}

func TestRequiredStringRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := requiredString(map[string]any{"name": ""}, "name")
	assertFieldError(t, toolErrorResult(err), "name")
	_, err = requiredString(map[string]any{"name": 7.0}, "name")
	assertFieldError(t, toolErrorResult(err), "name")
}

func TestOptionalFloatPtrEdges(t *testing.T) {
	t.Parallel()
	if v, err := optionalFloatPtr(map[string]any{"q": 3}, "q"); err != nil || v == nil || *v != 3 {
		t.Fatalf("int input: %v, %v", v, err)
	}
	if _, err := optionalFloatPtr(map[string]any{"q": -1.5}, "q"); err == nil {
		t.Fatal("negative must be rejected")
	}
	if v, err := optionalFloatPtr(map[string]any{"q": nil}, "q"); err != nil || v != nil {
		t.Fatalf("explicit null is absent: %v, %v", v, err)
	}
}
