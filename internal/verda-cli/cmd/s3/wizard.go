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
	"errors"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

const (
	defaultProfileName = "default"
	defaultRegion      = "us-east-1"
)

// buildConfigureFlow builds the interactive wizard flow for S3 credential configuration.
//
//	profile → access-key → secret-key → endpoint → region
func buildConfigureFlow(opts *configureOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "s3-configure",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			configureStepProfile(opts),
			configureStepAccessKey(opts),
			configureStepSecretKey(opts),
			configureStepEndpoint(opts),
			configureStepRegion(opts),
		},
	}
}

func configureStepProfile(opts *configureOptions) wizard.Step {
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
