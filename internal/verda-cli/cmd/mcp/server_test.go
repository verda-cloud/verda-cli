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
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// resultText extracts the text payload of a single-content tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

// assertToolErrorCode asserts the result is an error carrying the given
// agent-contract code in its JSON envelope.
func assertToolErrorCode(t *testing.T, res *mcp.CallToolResult, code string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("expected error result, got success: %s", resultText(t, res))
	}
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	text := resultText(t, res)
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("error payload is not the contract envelope: %v\ntext: %s", err, text)
	}
	if env.Error.Code != code {
		t.Fatalf("code = %q, want %q\ntext: %s", env.Error.Code, code, text)
	}
}

func TestRequiredString(t *testing.T) {
	a := map[string]any{"name": "test"}

	val, err := requiredString(a, "name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "test" {
		t.Errorf("got %q, want %q", val, "test")
	}

	_, err = requiredString(a, "missing")
	if err == nil {
		t.Fatal("expected error for missing arg")
	}
	var ae *argError
	if errors.As(err, &ae) && ae.code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q, want MISSING_REQUIRED_FLAGS", ae.code)
	}

	a["num"] = float64(7)
	_, err = requiredString(a, "num")
	if err == nil {
		t.Fatal("expected error for non-string arg")
	}
	if errors.As(err, &ae) && ae.code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", ae.code)
	}
}

func TestOptionalStringStrict(t *testing.T) {
	a := map[string]any{"str": "hello", "num": float64(1)}

	if got, err := optionalString(a, "str"); err != nil || got != "hello" {
		t.Errorf("optionalString = %q, %v; want hello, nil", got, err)
	}
	if got, err := optionalString(a, "missing"); err != nil || got != "" {
		t.Errorf("optionalString(missing) = %q, %v; want empty, nil", got, err)
	}
	if _, err := optionalString(a, "num"); err == nil {
		t.Error("optionalString(number) = nil error, want VALIDATION_ERROR")
	}
}

func TestOptionalBoolStrict(t *testing.T) {
	a := map[string]any{"flag": true, "str": "true"}

	if got, err := optionalBool(a, "flag"); err != nil || !got {
		t.Errorf("optionalBool = %v, %v; want true, nil", got, err)
	}
	if got, err := optionalBool(a, "missing"); err != nil || got {
		t.Errorf("optionalBool(missing) = %v, %v; want false, nil", got, err)
	}
	if _, err := optionalBool(a, "str"); err == nil {
		t.Error(`optionalBool("true" string) = nil error, want VALIDATION_ERROR`)
	}
}

func TestNumbersStrict(t *testing.T) {
	a := map[string]any{
		"json_num": float64(42),
		"go_int":   7,
		"str_num":  "500",
		"frac":     float64(1.5),
		"neg":      float64(-10),
		"bool":     true,
	}

	if got, err := optionalInt(a, "json_num"); err != nil || got != 42 {
		t.Errorf("optionalInt = %d, %v; want 42, nil", got, err)
	}
	if got, err := optionalInt(a, "go_int"); err != nil || got != 7 {
		t.Errorf("optionalInt(int) = %d, %v; want 7, nil", got, err)
	}
	if got, err := optionalInt(a, "missing"); err != nil || got != 0 {
		t.Errorf("optionalInt(missing) = %d, %v; want 0, nil", got, err)
	}

	// The review repro: a string "500" must not silently become 0/50GB default.
	for _, name := range []string{"str_num", "frac", "neg", "bool"} {
		if _, err := optionalInt(a, name); err == nil {
			t.Errorf("optionalInt(%s) = nil error, want rejection", name)
		}
	}

	if _, err := requiredInt(a, "missing_int"); err == nil {
		t.Error("requiredInt(missing) = nil error, want MISSING_REQUIRED_FLAGS")
	}
	if got, err := requiredInt(a, "json_num"); err != nil || got != 42 {
		t.Errorf("requiredInt = %d, %v; want 42, nil", got, err)
	}
}

func TestOptionalStringSliceStrict(t *testing.T) {
	a := map[string]any{
		"arr":      []any{"a", "b"},
		"string":   "abc",
		"mixed":    []any{"a", float64(2)},
		"empty_ok": []any{},
	}

	if got, err := optionalStringSlice(a, "arr"); err != nil || len(got) != 2 {
		t.Errorf("optionalStringSlice = %v, %v", got, err)
	}
	if got, err := optionalStringSlice(a, "missing"); err != nil || got != nil {
		t.Errorf("optionalStringSlice(missing) = %v, %v", got, err)
	}
	// The review repro: a bare string must not fall through to nil (create_vm
	// would then attach ALL account SSH keys).
	if got, err := optionalStringSlice(a, "string"); err == nil || got != nil {
		t.Errorf("optionalStringSlice(string) = %v, %v; want nil, error", got, err)
	}
	if _, err := optionalStringSlice(a, "mixed"); err == nil {
		t.Error("optionalStringSlice(mixed types) = nil error, want VALIDATION_ERROR")
	}
	if got, err := optionalStringSlice(a, "empty_ok"); err != nil || len(got) != 0 {
		t.Errorf("optionalStringSlice(empty) = %v, %v; want [], nil", got, err)
	}
}

func TestOptionalEnum(t *testing.T) {
	a := map[string]any{"type": "NVMe", "bad": "SCSI", "num": float64(1)}

	if got, err := optionalEnum(a, "type", "NVMe", "HDD"); err != nil || got != "NVMe" {
		t.Errorf("optionalEnum = %q, %v; want NVMe, nil", got, err)
	}
	if got, err := optionalEnum(a, "missing", "NVMe", "HDD"); err != nil || got != "" {
		t.Errorf("optionalEnum(missing) = %q, %v; want empty, nil", got, err)
	}
	if _, err := optionalEnum(a, "bad", "NVMe", "HDD"); err == nil {
		t.Error("optionalEnum(SCSI) = nil error, want VALIDATION_ERROR with valid list")
	}
	if _, err := optionalEnum(a, "num", "NVMe"); err == nil {
		t.Error("optionalEnum(number) = nil error, want VALIDATION_ERROR")
	}
}

func TestToolErrorResultEnvelope(t *testing.T) {
	res := toolErrorResult(confirmationRequiredError("create_vm"))
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), &env); err != nil {
		t.Fatalf("not the contract envelope: %v", err)
	}
	if env.Error.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q, want CONFIRMATION_REQUIRED", env.Error.Code)
	}
	if env.Error.Details["action"] != "create_vm" {
		t.Errorf("details.action = %v, want create_vm", env.Error.Details["action"])
	}

	// Non-argError falls back to plain text.
	res = toolErrorResult(errors.New("boom"))
	if !res.IsError || resultText(t, res) != "boom" {
		t.Errorf("plain error = %q, IsError=%v; want boom, true", resultText(t, res), res.IsError)
	}
}

func TestJSONResult(t *testing.T) {
	data := map[string]string{"key": "value"}
	result, err := jsonResult(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("result should not be error")
	}
	// Verify the content is valid JSON.
	for _, content := range result.Content {
		if tc, ok := content.(mcp.TextContent); ok {
			var parsed map[string]string
			if err := json.Unmarshal([]byte(tc.Text), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if parsed["key"] != "value" {
				t.Errorf("got %q, want %q", parsed["key"], "value")
			}
		}
	}
}

func TestNewServer(t *testing.T) {
	// Verify NewServer doesn't panic with a nil client.
	// This tests tool registration only.
	s := NewServer(nil)
	if s.mcpServer == nil {
		t.Fatal("mcpServer should not be nil")
	}
}
