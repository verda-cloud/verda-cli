package auth

import (
	"fmt"
	"strings"

	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

const defaultBaseURL = "https://api.verda.com/v1"

// buildLoginFlow builds the interactive wizard flow for auth login.
//
//	profile → base-url → client-id → client-secret
func buildLoginFlow(opts *loginOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "auth-login",
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
		Default:     func(_ map[string]any) any { return "default" },
		Validate: func(v any) error {
			if strings.TrimSpace(v.(string)) == "" {
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
				return fmt.Errorf("base URL cannot be empty")
			}
			if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
				return fmt.Errorf("base URL must start with http:// or https://")
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
