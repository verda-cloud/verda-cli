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

package options

import (
	"os"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

// RegistryCredentials holds Verda Container Registry credentials loaded
// from the shared credentials file. These are stored alongside API
// credentials using verda_registry_ prefixed keys.
type RegistryCredentials struct {
	Username  string
	Secret    string
	Endpoint  string // host only, e.g. "vccr.io"
	ProjectID string
	ExpiresAt time.Time // zero == non-expiring / unknown
}

// HasCredentials returns true if the minimum required registry credentials are set.
func (c *RegistryCredentials) HasCredentials() bool {
	return c.Username != "" && c.Secret != "" && c.Endpoint != ""
}

// IsExpired reports whether credentials are past their expiry. A zero
// ExpiresAt is treated as "not expired" (unknown / legacy entries).
func (c *RegistryCredentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

// DaysRemaining returns whole days until expiry. Negative if expired.
// Returns a large sentinel value when ExpiresAt is zero (non-expiring).
func (c *RegistryCredentials) DaysRemaining() int {
	if c.ExpiresAt.IsZero() {
		return 1 << 30
	}
	return int(time.Until(c.ExpiresAt).Hours() / 24)
}

// LoadRegistryCredentialsForProfile loads registry credentials for a specific profile.
func LoadRegistryCredentialsForProfile(path, profile string) (*RegistryCredentials, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}

	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	section, err := cfg.GetSection(profile)
	if err != nil {
		return nil, profileNotFoundError(cfg, profile, path)
	}

	creds := &RegistryCredentials{
		Username:  strings.TrimSpace(section.Key("verda_registry_username").String()),
		Secret:    strings.TrimSpace(section.Key("verda_registry_secret").String()),
		Endpoint:  strings.TrimSpace(section.Key("verda_registry_endpoint").String()),
		ProjectID: strings.TrimSpace(section.Key("verda_registry_project_id").String()),
	}
	if raw := strings.TrimSpace(section.Key("verda_registry_expires_at").String()); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			creds.ExpiresAt = t
		}
	}
	return creds, nil
}
