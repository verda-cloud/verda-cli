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

package util

import (
	"errors"
	"fmt"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
)

// ValidateHostname checks whether s is a valid hostname.
// Matches the web frontend regex: ^(?!-)(?!.*-$)[a-zA-Z0-9-]*[a-zA-Z][a-zA-Z0-9-]*$
// Go's regexp doesn't support lookaheads, so we validate with plain logic.
func ValidateHostname(s string) error {
	if s == "" {
		return errors.New("hostname cannot be empty")
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return errors.New("hostname must not start or end with a hyphen")
	}
	hasLetter := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			hasLetter = true
		} else if (c < '0' || c > '9') && c != '-' {
			return errors.New("hostname must contain only letters, digits, and hyphens")
		}
	}
	if !hasLetter {
		return errors.New("hostname must contain at least one letter")
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
