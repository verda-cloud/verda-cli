package options

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/verda-cloud/verdagostack/pkg/log"
)

const FlagConfig = "config"

const (
	defaultBaseURL            = "https://api.verda.com/v1"
	defaultCredentialsProfile = "default"
)

// Options holds the shared CLI configuration that is resolved once in the
// root command and made available to all subcommands through the Factory.
type Options struct {
	Config  string
	Server  string
	Timeout time.Duration
	Debug   bool
	Output  string
	Agent   bool

	Log         *log.Options
	AuthOptions *AuthOptions
}

// AuthOptions holds API authentication-related options.
type AuthOptions struct {
	ClientID        string
	ClientSecret    string
	BearerToken     string
	Profile         string
	CredentialsFile string

	resolveErr error
}

// NewOptions returns Options with sensible defaults.
func NewOptions() *Options {
	logOpts := log.NewOptions()
	logOpts.EnableColor = true

	return &Options{
		Server:      defaultBaseURL,
		Timeout:     30 * time.Second,
		Output:      "table",
		Log:         logOpts,
		AuthOptions: &AuthOptions{},
	}
}

// AddFlags binds the shared flags to the given flag set.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Config, FlagConfig, o.Config, "Path to a verda config file (YAML)")
	fs.StringVar(&o.Server, "base-url", o.Server, "API base URL")
	fs.DurationVar(&o.Timeout, "timeout", o.Timeout, "Default HTTP request timeout")
	fs.BoolVar(&o.Debug, "debug", false, "Enable debug output")
	fs.StringVarP(&o.Output, "output", "o", o.Output, "Output format: table, json, yaml")
	fs.BoolVar(&o.Agent, "agent", false, "Agent mode: JSON output, no interactive prompts, structured errors")
	o.AuthOptions.AddFlags(fs)
	o.Log.AddFlags(fs)

	// Hide verbose flags from help — they still work, just don't clutter output.
	hideFlags(fs, "auth.client-id", "auth.client-secret", "auth.token",
		"auth.profile", "auth.credentials-file",
		"log.disable-caller", "log.disable-stacktrace", "log.enable-color",
		"log.format", "log.level", "log.output-paths")
}

// AddFlags binds authentication flags to the given flag set.
func (o *AuthOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClientID, "auth.client-id", o.ClientID, "Verda API client ID")
	fs.StringVar(&o.ClientSecret, "auth.client-secret", o.ClientSecret, "Verda API client secret")
	fs.StringVar(&o.BearerToken, "auth.token", o.BearerToken, "Bearer token to send with API requests")
	fs.StringVar(&o.Profile, "auth.profile", o.Profile, "Shared credentials profile to use from ~/.verda/credentials")
	fs.StringVar(&o.CredentialsFile, "auth.credentials-file", o.CredentialsFile, "Path to a shared credentials file in AWS-style INI format")
}

func hideFlags(fs *pflag.FlagSet, names ...string) {
	for _, name := range names {
		if f := fs.Lookup(name); f != nil {
			f.Hidden = true
		}
	}
}

// Complete fills in zero-value fields from viper (config file / env).
//
//nolint:gocyclo // Configuration resolution checks many sources (flags, env, config, credentials) — inherently sequential.
func (o *Options) Complete() {
	if o.Config == "" {
		o.Config = viper.GetString(FlagConfig)
	}
	if o.Server == "" {
		o.Server = viper.GetString("base-url")
	}
	if o.Timeout == 0 {
		o.Timeout = viper.GetDuration("timeout")
	}
	if o.Output == "" {
		o.Output = viper.GetString("output")
	}
	if o.Output == "" {
		o.Output = "table"
	}
	if !o.Agent {
		o.Agent = viper.GetBool("agent")
	}
	if !o.Agent {
		o.Agent = os.Getenv("VERDA_AGENT") == "1" || os.Getenv("VERDA_AGENT") == "true"
	}
	if o.Agent {
		o.Output = "json"
	}
	a := o.AuthOptions

	// --- 1. Resolve credentials file first (needed by profile resolution). ---
	if a.CredentialsFile == "" {
		a.CredentialsFile = viper.GetString("auth.credentials-file")
	}
	if a.CredentialsFile == "" {
		a.CredentialsFile = os.Getenv("VERDA_SHARED_CREDENTIALS_FILE")
	}
	if a.CredentialsFile == "" {
		if path, err := DefaultCredentialsFilePath(); err == nil {
			a.CredentialsFile = path
		}
	}

	// --- 2. Resolve profile. Track whether it was explicitly set. ---
	// Priority: flag > env var > config file > auto-detect.
	// Only flag and env var count as "explicit" (runtime override that
	// should force-load credentials from that profile).
	explicitProfile := a.Profile != "" // set via --auth.profile flag
	if a.Profile == "" {
		if p := os.Getenv("VERDA_PROFILE"); p != "" {
			a.Profile = p
			explicitProfile = true
		}
	}
	if a.Profile == "" {
		a.Profile = viper.GetString("auth.profile") // config file (saved default)
	}
	if a.Profile == "" {
		a.Profile = resolveDefaultProfile(a.CredentialsFile)
	}

	// --- 3. Resolve inline credentials (flags / env). ---
	// Track which fields were set explicitly so they win over profile values.
	flagClientID := a.ClientID != ""
	if a.ClientID == "" {
		a.ClientID = viper.GetString("auth.client-id")
	}
	if a.ClientID == "" {
		a.ClientID = os.Getenv("VERDA_CLIENT_ID")
	}
	flagClientSecret := a.ClientSecret != ""
	if a.ClientSecret == "" {
		a.ClientSecret = viper.GetString("auth.client-secret")
	}
	if a.ClientSecret == "" {
		a.ClientSecret = os.Getenv("VERDA_CLIENT_SECRET")
	}
	flagToken := a.BearerToken != ""
	if a.BearerToken == "" {
		a.BearerToken = viper.GetString("auth.token")
	}

	// --- 4. Load from credentials file. ---
	// When the profile was explicitly chosen, its credentials override any
	// auto-resolved values — but explicit flags/env always win.
	missingRequired := a.ClientID == "" || a.ClientSecret == ""
	if explicitProfile || missingRequired || a.BearerToken == "" {
		shared, err := loadSharedCredentials(a.CredentialsFile, a.Profile)
		switch {
		case err == nil:
			if shared.BaseURL != "" && (explicitProfile || o.Server == defaultBaseURL) {
				o.Server = shared.BaseURL
			}
			if shared.ClientID != "" && (explicitProfile && !flagClientID || a.ClientID == "") {
				a.ClientID = shared.ClientID
			}
			if shared.ClientSecret != "" && (explicitProfile && !flagClientSecret || a.ClientSecret == "") {
				a.ClientSecret = shared.ClientSecret
			}
			if shared.BearerToken != "" && (explicitProfile && !flagToken || a.BearerToken == "") {
				a.BearerToken = shared.BearerToken
			}
		case (explicitProfile || missingRequired) && !os.IsNotExist(err):
			a.resolveErr = err
		}
	}
}

// Validate checks that the options are consistent.
func (o *Options) Validate() error {
	if o.AuthOptions != nil && o.AuthOptions.resolveErr != nil {
		return o.AuthOptions.resolveErr
	}
	if o.Server == "" {
		return errors.New("--base-url must not be empty")
	}
	if o.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	switch o.Output {
	case "table", "json", "yaml":
	default:
		return fmt.Errorf("--output must be one of: table, json, yaml (got %q)", o.Output)
	}
	return nil
}

// resolveDefaultProfile picks the best profile when none is explicitly set.
// It prefers "default" if it exists, otherwise uses the sole profile if there
// is exactly one, and falls back to "default" as a last resort.
func resolveDefaultProfile(credentialsFile string) string {
	profiles, err := ListProfiles(credentialsFile)
	if err != nil || len(profiles) == 0 {
		return defaultCredentialsProfile
	}

	// If "default" profile exists, use it.
	for _, p := range profiles {
		if p == defaultCredentialsProfile {
			return defaultCredentialsProfile
		}
	}

	// If there's exactly one profile, use it automatically.
	if len(profiles) == 1 {
		return profiles[0]
	}

	// Multiple profiles, none named "default" — fall back and let the
	// validation error guide the user to 'verda auth use'.
	return defaultCredentialsProfile
}
