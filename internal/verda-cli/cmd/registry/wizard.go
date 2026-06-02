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
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// newProfileSentinel is the Choice value for "create a new profile". A NUL
// byte can't occur in an INI section name, so it never collides with a real
// profile. Mirrors the s3 wizard's sentinel.
const newProfileSentinel = "\x00new-profile"

// Credential input modes offered by the wizard, matching the two ways the web
// UI lets you copy a credential: the whole `docker login …` command, or the
// "Full credential name" + "Secret" fields separately.
const (
	inputModePaste  = "paste"
	inputModeManual = "manual"
)

// buildConfigureFlow builds the interactive wizard flow for registry
// credential configuration.
//
//	profile (pick or create) → [new name] → input mode
//	  ├─ paste:  paste docker-login command
//	  └─ manual: username → secret
//	→ expires-in → docker-config
//
// The paste step asks the user to paste the full `docker login ...` command
// the Verda web UI prints when a credential is provisioned. Its validator
// routes the string through parseDockerLogin so the user sees the parser's
// diagnostic in-place and can try again without restarting the flow. The
// manual path mirrors the web UI's separate "Full credential name" + "Secret"
// fields; the host isn't asked for because it's derived in configure.go (the
// project id is parsed out of the credential name, the base is vccr.io / the
// profile's saved host). Both paths populate opts so the flag-driven
// resolution path in configure.go handles persistence.
func buildConfigureFlow(opts *configureOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "registry-configure",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			configureStepProfile(opts),
			configureStepNewProfileName(opts),
			configureStepInputMode(),
			configureStepPaste(opts),
			configureStepUsername(opts),
			configureStepSecret(opts),
			configureStepExpiresIn(opts),
			configureStepDockerConfig(opts),
		},
	}
}

// configureStepInputMode asks whether the user has the full `docker login`
// command (paste) or the credential name + secret separately (manual). The
// chosen value is read by the paste / username / secret / endpoint steps'
// ShouldSkip predicates via collected["input-mode"].
func configureStepInputMode() wizard.Step {
	return wizard.Step{
		Name:        "input-mode",
		Description: "How do you have the credential?",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(_ context.Context, _ tui.Prompter, _ tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			return []wizard.Choice{
				{Label: "Paste the docker login command (recommended)", Value: inputModePaste},
				{Label: "Enter credential name + secret", Value: inputModeManual},
			}, nil
		},
		Default:  func(_ map[string]any) any { return inputModePaste },
		Setter:   func(any) {},
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return inputModePaste },
	}
}

// configureStepUsername (manual path) prompts for the "Full credential name"
// the web UI shows, validating the vcr-<project-id>+<name> shape so the user
// sees a clear error before the flow moves on. Resetter is a no-op so that, in
// paste mode where this step is skipped, it doesn't clobber the username the
// paste step parsed onto opts.
func configureStepUsername(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "username",
		Description: "Full credential name (vcr-<project-id>+<name>)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"input-mode"},
		ShouldSkip: func(collected map[string]any) bool {
			v, _ := collected["input-mode"].(string)
			return v != inputModeManual
		},
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return errors.New("credential name cannot be empty")
			}
			if _, err := projectIDFromUsername(s); err != nil {
				return err
			}
			return nil
		},
		Setter:   func(v any) { opts.Username = strings.TrimSpace(v.(string)) },
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.Username },
	}
}

// configureStepSecret (manual path) prompts for the "Secret" field. Masked
// input; the value is stored verbatim (not trimmed) since a secret may contain
// surrounding whitespace. No-op Resetter for the same reason as username.
func configureStepSecret(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "secret",
		Description: "Secret",
		Prompt:      wizard.PasswordPrompt,
		Required:    true,
		DependsOn:   []string{"input-mode"},
		ShouldSkip: func(collected map[string]any) bool {
			v, _ := collected["input-mode"].(string)
			return v != inputModeManual
		},
		Validate: func(v any) error {
			if v.(string) == "" {
				return errors.New("secret cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.Secret = v.(string) },
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.Secret },
	}
}

// registryProfileChoices lists existing credential profiles (each tagged with
// whether it already holds registry credentials) plus a trailing "create new"
// option. Reading the file fails soft: on any error the user still gets the
// create-new choice. Mirrors the s3 wizard's profileChoices.
func registryProfileChoices(path string) []wizard.Choice {
	profiles, _ := options.ListProfiles(path)
	choices := make([]wizard.Choice, 0, len(profiles)+1)
	for _, p := range profiles {
		desc := "no registry credentials yet"
		if creds, err := options.LoadRegistryCredentialsForProfile(path, p); err == nil && creds.HasCredentials() {
			desc = "registry configured"
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
			return registryProfileChoices(credentialsFilePath(opts.CredentialsFile)), nil
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
		// (called when this step is skipped for an existing profile) would
		// clobber the selected profile back to the default.
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return opts.Profile },
	}
}

// configureStepPaste asks the user to paste the full `docker login ...`
// command from the Verda web UI and routes it through parseDockerLogin. On
// success the parsed fields populate opts so the existing persistence path
// in runConfigure works unchanged.
//
// The description names the exact UI field ("Registry authentication
// command") so the user knows what to look for on the credential-created
// dialog, rather than hunting for a free-form "docker login" string.
func configureStepPaste(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "paste",
		Description: "Paste the 'Registry authentication command' from the Verda UI (docker login -u … -p … <host>)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"input-mode"},
		ShouldSkip: func(collected map[string]any) bool {
			v, _ := collected["input-mode"].(string)
			return v == inputModeManual
		},
		Validate: func(v any) error {
			s, _ := v.(string)
			if strings.TrimSpace(s) == "" {
				return errors.New("paste cannot be empty")
			}
			if _, err := parseDockerLogin(s); err != nil {
				return err
			}
			return nil
		},
		Setter: func(v any) {
			s, _ := v.(string)
			parsed, err := parseDockerLogin(s)
			if err != nil {
				// Validate ran already, but guard defensively.
				return
			}
			opts.Paste = s
			opts.Username = parsed.Username
			opts.Endpoint = parsed.Host
			// opts has no ProjectID field; the flag-driven path pulls it from
			// parseDockerLogin inside resolveRegistryInputs when --paste is
			// populated, so we just need Paste set for that to happen.
			_ = parsed.ProjectID
			_ = parsed.Secret
		},
		Resetter: func() {
			opts.Paste = ""
			opts.Username = ""
			opts.Endpoint = ""
		},
		IsSet: func() bool { return strings.TrimSpace(opts.Paste) != "" },
		Value: func() any { return opts.Paste },
	}
}

// configureStepExpiresIn asks for the expiry window in days. Defaults to
// defaultExpiresInDays (30) and accepts any non-negative integer. Zero is
// allowed and means "no expiry" per computeExpiry.
func configureStepExpiresIn(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "expires-in",
		Description: "Expires in (days)",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			return strconv.Itoa(defaultExpiresInDays)
		},
		Validate: func(v any) error {
			s, _ := v.(string)
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("expires-in cannot be empty")
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				return fmt.Errorf("expires-in must be an integer: %w", err)
			}
			if n < 0 {
				return errors.New("expires-in must be >= 0")
			}
			return nil
		},
		Setter: func(v any) {
			s, _ := v.(string)
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return
			}
			opts.ExpiresInDays = n
		},
		Resetter: func() { opts.ExpiresInDays = defaultExpiresInDays },
		IsSet: func() bool {
			// Only treat as preset when user actually overrode the flag
			// (different from our default).
			return opts.ExpiresInDays != defaultExpiresInDays
		},
		Value: func() any { return strconv.Itoa(opts.ExpiresInDays) },
	}
}

// configureStepDockerConfig asks whether to also write ~/.docker/config.json.
// When true, runConfigure performs the same merge `verda registry configure-docker` does
// (via writeDockerLogin) right after saving the Verda credentials.
func configureStepDockerConfig(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "docker-config",
		Description: "Also write ~/.docker/config.json for use with docker pull / docker-compose?",
		Prompt:      wizard.ConfirmPrompt,
		Default: func(_ map[string]any) any {
			return true
		},
		Setter: func(v any) {
			b, _ := v.(bool)
			opts.DockerConfig = b
		},
		Resetter: func() { opts.DockerConfig = false },
		IsSet:    func() bool { return opts.DockerConfig },
		Value:    func() any { return opts.DockerConfig },
	}
}
