package util

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteStructuredJSON(t *testing.T) {
	t.Parallel()

	data := map[string]string{"name": "test-vol", "id": "abc-123"}
	var buf bytes.Buffer

	wrote, err := WriteStructured(&buf, "json", data)
	if err != nil {
		t.Fatalf("WriteStructured(json) error: %v", err)
	}
	if !wrote {
		t.Fatal("WriteStructured(json) returned false, want true")
	}

	var decoded map[string]string
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded["name"] != "test-vol" || decoded["id"] != "abc-123" {
		t.Fatalf("unexpected decoded output: %v", decoded)
	}
}

func TestWriteStructuredYAML(t *testing.T) {
	t.Parallel()

	data := map[string]string{"name": "test-vol", "id": "abc-123"}
	var buf bytes.Buffer

	wrote, err := WriteStructured(&buf, "yaml", data)
	if err != nil {
		t.Fatalf("WriteStructured(yaml) error: %v", err)
	}
	if !wrote {
		t.Fatal("WriteStructured(yaml) returned false, want true")
	}

	out := buf.String()
	if !strings.Contains(out, "name: test-vol") {
		t.Fatalf("expected YAML output to contain 'name: test-vol', got: %s", out)
	}
	if !strings.Contains(out, "id: abc-123") {
		t.Fatalf("expected YAML output to contain 'id: abc-123', got: %s", out)
	}
}

func TestWriteStructuredJSONPrettyPrinted(t *testing.T) {
	t.Parallel()

	data := map[string]int{"count": 42}
	var buf bytes.Buffer

	_, _ = WriteStructured(&buf, "json", data)

	// Should be indented (pretty-printed).
	if !strings.Contains(buf.String(), "\n") {
		t.Fatal("expected pretty-printed JSON with newlines")
	}
}

func TestWriteStructuredTableReturnsFalse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	wrote, err := WriteStructured(&buf, "table", "anything")
	if err != nil {
		t.Fatalf("WriteStructured(table) error: %v", err)
	}
	if wrote {
		t.Fatal("WriteStructured(table) returned true, want false")
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for table format, got: %s", buf.String())
	}
}

func TestWriteStructuredSlice(t *testing.T) {
	t.Parallel()

	data := []map[string]string{
		{"name": "a"},
		{"name": "b"},
	}
	var buf bytes.Buffer

	wrote, err := WriteStructured(&buf, "json", data)
	if err != nil {
		t.Fatalf("WriteStructured(json, slice) error: %v", err)
	}
	if !wrote {
		t.Fatal("expected true")
	}

	// Verify it's a JSON array.
	out := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(out, "[") || !strings.HasSuffix(out, "]") {
		t.Fatalf("expected JSON array, got: %s", out)
	}
}
