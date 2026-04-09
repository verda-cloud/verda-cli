package util

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateHostname(t *testing.T) {
	valid := []string{
		"cold-cable-smiles-fin-01",
		"my-host",
		"a",
		"host123",
		"123host",
		"a-b-c",
	}
	for _, h := range valid {
		if err := ValidateHostname(h); err != nil {
			t.Errorf("ValidateHostname(%q) = %v, want nil", h, err)
		}
	}

	invalid := []struct {
		input, wantContains string
	}{
		{"", "empty"},
		{"-bad", "start"},
		{"bad-", "end"},
		{"123-456", "letter"},
		{"bad_host", "letters, digits, and hyphens"},
		{"has space", "letters, digits, and hyphens"},
	}
	for _, tc := range invalid {
		err := ValidateHostname(tc.input)
		if err == nil {
			t.Errorf("ValidateHostname(%q) = nil, want error containing %q", tc.input, tc.wantContains)
		} else if !strings.Contains(err.Error(), tc.wantContains) {
			t.Errorf("ValidateHostname(%q) = %v, want error containing %q", tc.input, err, tc.wantContains)
		}
	}
}

func TestGenerateHostname(t *testing.T) {
	h := GenerateHostname("FIN-01")
	fmt.Printf("Generated: %s\n", h)

	if !strings.HasSuffix(h, "-fin-01") {
		t.Errorf("GenerateHostname(FIN-01) = %q, want suffix -fin-01", h)
	}
	if err := ValidateHostname(h); err != nil {
		t.Errorf("GenerateHostname produced invalid hostname %q: %v", h, err)
	}

	// Should have at least 3 dashes (3 words + location).
	parts := strings.Split(h, "-")
	if len(parts) < 5 { // 3 words + "fin" + "01"
		t.Errorf("GenerateHostname(FIN-01) = %q, expected at least 5 parts separated by dashes", h)
	}
}
