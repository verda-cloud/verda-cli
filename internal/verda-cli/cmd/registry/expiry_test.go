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

package registry

import (
	"errors"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// validCreds returns a RegistryCredentials with the minimum fields populated
// so HasCredentials() returns true. Tests override ExpiresAt individually.
func validCreds(expiresAt time.Time) *options.RegistryCredentials {
	return &options.RegistryCredentials{
		Username:  "robot$verda",
		Secret:    "s3cret",
		Endpoint:  "vccr.io",
		ProjectID: "proj-123",
		ExpiresAt: expiresAt,
	}
}

func TestCheckExpiry_TableDriven(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name     string
		creds    *options.RegistryCredentials
		wantKind string // empty == expect nil
	}{
		{
			name:     "nil creds returns not_configured",
			creds:    nil,
			wantKind: kindRegistryNotConfigured,
		},
		{
			name:     "zero-value creds returns not_configured",
			creds:    &options.RegistryCredentials{},
			wantKind: kindRegistryNotConfigured,
		},
		{
			name:     "valid creds with zero ExpiresAt returns nil (legacy bypass)",
			creds:    validCreds(time.Time{}),
			wantKind: "",
		},
		{
			name:     "valid creds with past ExpiresAt returns credential_expired",
			creds:    validCreds(now.Add(-24 * time.Hour)),
			wantKind: kindRegistryCredentialExpired,
		},
		{
			name:     "valid creds with future ExpiresAt returns nil",
			creds:    validCreds(now.Add(24 * time.Hour)),
			wantKind: "",
		},
		{
			name:     "valid creds expired one second ago returns credential_expired",
			creds:    validCreds(now.Add(-1 * time.Second)),
			wantKind: kindRegistryCredentialExpired,
		},
		{
			name:     "valid creds expiring one second from now returns nil",
			creds:    validCreds(now.Add(1 * time.Second)),
			wantKind: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := checkExpiry(tc.creds)

			if tc.wantKind == "" {
				if err != nil {
					t.Fatalf("checkExpiry: expected nil, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("checkExpiry: expected AgentError kind %q, got nil", tc.wantKind)
			}
			var ae *cmdutil.AgentError
			if !errors.As(err, &ae) {
				t.Fatalf("checkExpiry: expected *cmdutil.AgentError, got %T: %v", err, err)
			}
			if ae.Code != tc.wantKind {
				t.Fatalf("checkExpiry: expected kind %q, got %q (msg=%q)", tc.wantKind, ae.Code, ae.Message)
			}
		})
	}
}

// TestCheckExpiry_ExpiredMessageContainsDate pins the message format for the
// expired path: the date is formatted as YYYY-MM-DD (UTC, deterministic) and
// the remediation string references `verda registry configure`.
func TestCheckExpiry_ExpiredMessageContainsDate(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	creds := validCreds(expiresAt)

	err := checkExpiry(creds)
	if err == nil {
		t.Fatal("checkExpiry: expected error for expired creds, got nil")
	}

	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("checkExpiry: expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryCredentialExpired {
		t.Fatalf("checkExpiry: expected kind %q, got %q", kindRegistryCredentialExpired, ae.Code)
	}
	if !strings.Contains(ae.Message, "2026-01-15") {
		t.Fatalf("checkExpiry: expected message to contain %q, got %q", "2026-01-15", ae.Message)
	}
	if !strings.Contains(ae.Message, "verda registry configure") {
		t.Fatalf("checkExpiry: expected message to contain %q, got %q", "verda registry configure", ae.Message)
	}
}

// TestCheckExpiry_NotConfiguredMessage pins the not-configured remediation
// string so UX doesn't silently regress.
func TestCheckExpiry_NotConfiguredMessage(t *testing.T) {
	t.Parallel()

	err := checkExpiry(nil)
	if err == nil {
		t.Fatal("checkExpiry(nil): expected error, got nil")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("checkExpiry(nil): expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryNotConfigured {
		t.Fatalf("checkExpiry(nil): expected kind %q, got %q", kindRegistryNotConfigured, ae.Code)
	}
	if !strings.Contains(ae.Message, "verda registry configure") {
		t.Fatalf("checkExpiry(nil): expected message to contain %q, got %q", "verda registry configure", ae.Message)
	}
}
