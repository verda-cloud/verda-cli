package auth

import (
	"fmt"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// buildConfigureFlow builds the interactive wizard flow for auth configure.
//
//	profile → client-id → client-secret → token (optional)
func buildConfigureFlow(opts *configureOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "auth-configure",
		Steps: []wizard.Step{
			stepProfile(opts),
			stepClientID(opts),
			stepClientSecret(opts),
			stepBearerToken(opts),
		},
	}
}

func stepProfile(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "profile",
		Description: "Profile name",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Default:     func(_ map[string]any) any { return "default" },
		Validate: func(v any) error {
			s := strings.TrimSpace(v.(string))
			if s == "" {
				return fmt.Errorf("profile name cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.Profile = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.Profile = "default" },
		IsSet:    func() bool { return opts.Profile != "" && opts.Profile != "default" },
		Value:    func() any { return opts.Profile },
	}
}

func stepClientID(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "client-id",
		Description: "Verda API client ID",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
				return fmt.Errorf("client ID cannot be empty")
			}
			return nil
		},
		Setter:   func(v any) { opts.ClientID = strings.TrimSpace(v.(string)) },
		Resetter: func() { opts.ClientID = "" },
		IsSet:    func() bool { return opts.ClientID != "" },
		Value:    func() any { return opts.ClientID },
	}
}

func stepClientSecret(opts *configureOptions) wizard.Step {
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

func stepBearerToken(opts *configureOptions) wizard.Step {
	return wizard.Step{
		Name:        "token",
		Description: "Bearer token (optional, press Enter to skip)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Setter:      func(v any) { opts.BearerToken = strings.TrimSpace(v.(string)) },
		Resetter:    func() { opts.BearerToken = "" },
		IsSet:       func() bool { return opts.BearerToken != "" },
		Value:       func() any { return opts.BearerToken },
	}
}
