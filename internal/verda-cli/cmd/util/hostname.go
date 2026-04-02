package util

import (
	"fmt"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
)

// ValidateHostname checks whether s is a valid hostname.
// Matches the web frontend regex: ^(?!-)(?!.*-$)[a-zA-Z0-9-]*[a-zA-Z][a-zA-Z0-9-]*$
// Go's regexp doesn't support lookaheads, so we validate with plain logic.
func ValidateHostname(s string) error {
	if s == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return fmt.Errorf("hostname must not start or end with a hyphen")
	}
	hasLetter := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		} else if (c < '0' || c > '9') && c != '-' {
			return fmt.Errorf("hostname must contain only letters, digits, hyphens and underscores")
		}
	}
	if !hasLetter {
		return fmt.Errorf("hostname must contain at least one letter")
	}
	return nil
}

// GenerateHostname returns a random hostname like "cold-cable-smiles-fin-01".
// It combines 3 random words with the location code (lowercased).
func GenerateHostname(locationCode string) string {
	words := petname.Generate(3, "-")
	loc := strings.ToLower(locationCode)
	return fmt.Sprintf("%s-%s", words, loc)
}
