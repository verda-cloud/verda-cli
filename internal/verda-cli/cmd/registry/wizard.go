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
	"fmt"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// buildConfigureFlow builds the interactive wizard flow for registry
// credential configuration.
//
//	paste → expires-in → docker-config
//
// The paste step asks the user to paste the full `docker login ...` command
// the Verda web UI prints when a credential is provisioned. Its validator
// routes the string through parseDockerLogin so the user sees the parser's
// diagnostic in-place and can try again without restarting the flow. On
// success the parsed username/secret/host/project-id are written directly
// onto opts so the existing flag-driven persistence code path in configure.go
// handles the rest.
func buildConfigureFlow(opts *configureOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "registry-configure",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
		},
		Steps: []wizard.Step{
			configureStepPaste(opts),
			configureStepExpiresIn(opts),
			configureStepDockerConfig(opts),
		},
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
// The actual write happens in `verda registry login` (Task 13); for now we
// only store the boolean and the RunE will print a notice so the user knows
// they still need to run login to materialize the docker config.
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
