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

package auth

import (
	"errors"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

const (
	defaultBaseURL     = "https://api.verda.com/v1"
	defaultProfileName = "default"
)

// buildLoginFlow builds the interactive wizard flow for auth login.
//
//	profile → base-url → client-id → client-secret
func buildLoginFlow(opts *loginOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "auth-login",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			loginStepProfile(opts),
			loginStepBaseURL(opts),
			loginStepClientID(opts),
			loginStepClientSecret(opts),
		},
	}
}

func loginStepProfile(opts *loginOptions) wizard.Step {
	return wizard.Step{
		Name:        "profile",
		Description: "Profile name",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return defaultProfileName },
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("profile name cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.Profile = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Profile = defaultProfileName },
		IsSet:    func() bool { return opts.Profile != "" && opts.Profile != defaultProfileName },
		Value:    func() any { return opts.Profile },
	}
}

func loginStepBaseURL(opts *loginOptions) wizard.Step {
	return wizard.Step{
		Name:        "base-url",
		Description: "API base URL",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return defaultBaseURL },
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return errors.New("base URL cannot be empty")
			}
			if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
				return errors.New("base URL must start with http:// or https://")
			}
			return nil
		},
		Setter:   func(v any) { opts.BaseURL = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.BaseURL = defaultBaseURL },
		IsSet:    func() bool { return opts.BaseURL != "" && opts.BaseURL != defaultBaseURL },
		Value:    func() any { return opts.BaseURL },
	}
}

func loginStepClientID(opts *loginOptions) wizard.Step {
	return wizard.Step{
		Name:        "client-id",
		Description: "Verda API client ID",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("client ID cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.ClientID = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.ClientID = "" },
		IsSet:    func() bool { return opts.ClientID != "" },
		Value:    func() any { return opts.ClientID },
	}
}

func loginStepClientSecret(opts *loginOptions) wizard.Step {
	return wizard.Step{
		Name:        "client-secret",
		Description: "Verda API client secret",
		Prompt:      wizard.PasswordPrompt,
		Required:    true,
		Setter:      func(v any) { opts.ClientSecret = v.(string) },
		Resetter:    func() { opts.ClientSecret = "" },
		IsSet:       func() bool { return opts.ClientSecret != "" },
		Value:       func() any { return opts.ClientSecret },
	}
}
