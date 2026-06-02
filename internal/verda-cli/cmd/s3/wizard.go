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

package s3

import (
	"context"
	"errors"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

const (
	defaultProfileName = "default"
	defaultRegion      = "us-east-1"
	// newProfileSentinel is the Choice value for "create a new profile". A NUL
	// byte can't occur in an INI section name, so it never collides with a real
	// profile.
	newProfileSentinel = "\x00new-profile"
)

// buildConfigureFlow builds the interactive wizard flow for S3 credential configuration.
//
//	profile (pick or create) → [new name] → access-key → secret-key → endpoint → region
func buildConfigureFlow(opts *configureOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "s3-configure",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			configureStepProfile(opts),
			configureStepNewProfileName(opts),
			configureStepAccessKey(opts),
			configureStepSecretKey(opts),
			configureStepEndpoint(opts),
			configureStepRegion(opts),
		},
	}
}

// profileChoices lists existing credential profiles (each tagged with whether it
// already holds S3 credentials) plus a trailing "create new" option. Reading the
// file fails soft: on any error the user still gets the create-new choice.
func profileChoices(path string) []wizard.Choice {
	profiles, _ := options.ListProfiles(path)
	choices := make([]wizard.Choice, 0, len(profiles)+1)
	for _, p := range profiles {
		desc := "no S3 credentials yet"
		if creds, err := options.LoadS3CredentialsForProfile(path, p); err == nil && creds.HasCredentials() {
			desc = "S3 configured"
		}
		choices = append(choices, wizard.Choice{Label: p, Value: p, Description: desc})
	}
	return append(choices, wizard.Choice{Label: "+ Create new profile…", Value: newProfileSentinel})
}

// configureStepProfile lets the user pick an existing profile or create a new
// one, defaulting the selection to the active profile. A --profile flag
// (opts.Profile != default) pre-sets it and skips this step.
func configureStepProfile(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "profile",
		Description: "Profile",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			// List the same file the save writes to (must honor --credentials-file).
			path, err := resolveCredentialsFile(opts.CredentialsFile)
			if err != nil {
				return nil, err
			}
			return profileChoices(path), nil
		},
		Default: func(_ map[string]any) any {
			if p := options.ActiveProfile(""); p != "" {
				return p
			}
			return defaultProfileName
		},
		// Sentinel ("create new") leaves opts.Profile alone; the new-name step
		// sets it. Picking an existing profile sets it directly.
		Setter: func(v any) {
			if s, _ := v.(string); s != "" && s != newProfileSentinel {
				opts.Profile = s
			}
		},
		Resetter: func() { opts.Profile = defaultProfileName },
		IsSet:    func() bool { return opts.Profile != "" && opts.Profile != defaultProfileName },
		Value:    func() any { return opts.Profile },
	}
}

// configureStepNewProfileName prompts for the name only when the user chose
// "create new" in the profile step.
func configureStepNewProfileName(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "new-profile-name",
		Description: "New profile name",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"profile"},
		ShouldSkip: func(collected map[string]any) bool {
			v, _ := collected["profile"].(string)
			return v != newProfileSentinel
		},
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("profile name cannot be empty")
			}
			return nil
		},
		Setter: func(v any) { opts.Profile = strings.TrimSpace(v.(string)) },
		// No-op: opts.Profile is owned by the profile step. Resetting it here
		// (called when this step is skipped for an existing profile) would clobber
		// the selected profile back to the default.
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.Profile },
	}
}

func configureStepAccessKey(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "access-key",
		Description: "S3 access key ID",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("access key cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.AccessKey = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.AccessKey = "" },
		IsSet:    func() bool { return opts.AccessKey != "" },
		Value:    func() any { return opts.AccessKey },
	}
}

func configureStepSecretKey(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "secret-key",
		Description: "S3 secret access key",
		Prompt:      wizard.PasswordPrompt,
		Required:    true,
		Setter:      func(v any) { opts.SecretKey = v.(string) },
		Resetter:    func() { opts.SecretKey = "" },
		IsSet:       func() bool { return opts.SecretKey != "" },
		Value:       func() any { return opts.SecretKey },
	}
}

func configureStepEndpoint(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "endpoint",
		Description: "S3 endpoint URL",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		// Pre-fill the production endpoint; the user accepts it with Enter or
		// types their own region's URL.
		Default: func(_ map[string]any) any { return DefaultEndpoint },
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return errors.New("endpoint URL cannot be empty")
			}
			if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
				return errors.New("endpoint URL must start with http:// or https://")
			}
			return nil
		},
		Setter:   func(v any) { opts.Endpoint = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Endpoint = "" },
		IsSet:    func() bool { return opts.Endpoint != "" },
		Value:    func() any { return opts.Endpoint },
	}
}

func configureStepRegion(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "region",
		Description: "S3 region",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return defaultRegion },
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return errors.New("region cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.Region = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Region = defaultRegion },
		IsSet:    func() bool { return opts.Region != "" && opts.Region != defaultRegion },
		Value:    func() any { return opts.Region },
	}
}
