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

package serverless

import (
	"testing"
)

func TestParsePositiveIntValidator(t *testing.T) {
	v := parsePositiveIntValidator("test field")
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"one", "1", false},
		{"hundred", "100", false},
		{"zero", "0", true},
		{"negative", "-1", true},
		{"letters", "abc", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

func TestParseNonNegativeIntValidator(t *testing.T) {
	v := parseNonNegativeIntValidator("test field")
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"zero", "0", false},
		{"one", "1", false},
		{"hundred", "100", false},
		{"negative", "-1", true},
		{"letters", "abc", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

func TestParsePortValidator(t *testing.T) {
	v := parsePortValidator("test port")
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"min", "1", false},
		{"common", "80", false},
		{"max", "65535", false},
		{"zero", "0", true},
		{"over max", "65536", true},
		{"negative", "-1", true},
		{"letters", "abc", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}
