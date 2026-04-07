package mcp

import (
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

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
}

func TestOptionalHelpers(t *testing.T) {
	a := map[string]any{
		"str":  "hello",
		"flag": true,
		"num":  float64(42),
	}

	if got := optionalString(a, "str"); got != "hello" {
		t.Errorf("optionalString = %q, want %q", got, "hello")
	}
	if got := optionalString(a, "missing"); got != "" {
		t.Errorf("optionalString(missing) = %q, want empty", got)
	}
	if got := optionalBool(a, "flag"); !got {
		t.Error("optionalBool = false, want true")
	}
	if got := optionalBool(a, "missing"); got {
		t.Error("optionalBool(missing) = true, want false")
	}
	if got := optionalInt(a, "num"); got != 42 {
		t.Errorf("optionalInt = %d, want 42", got)
	}
	if got := optionalInt(a, "missing"); got != 0 {
		t.Errorf("optionalInt(missing) = %d, want 0", got)
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
