package options

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/verda-cloud/verdagostack/pkg/log"
)

const FlagConfig = "config"

const (
	defaultCredentialsProfile = "default"
	defaultCredentialsPath    = ".verda/credentials"
)

// Options holds the shared CLI configuration that is resolved once in the
// root command and made available to all subcommands through the Factory.
type Options struct {
	Config  string
	Server  string
	Timeout time.Duration

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
		Server:      "https://api.verda.com/v1",
		Timeout:     30 * time.Second,
		Log:         logOpts,
		AuthOptions: &AuthOptions{},
	}
}

// AddFlags binds the shared flags to the given flag set.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Config, FlagConfig, o.Config, "Path to a verda config file (YAML)")
	fs.StringVar(&o.Server, "server", o.Server, "API base URL")
	fs.DurationVar(&o.Timeout, "timeout", o.Timeout, "Default HTTP request timeout")
	o.AuthOptions.AddFlags(fs)
	o.Log.AddFlags(fs)
}

// AddFlags binds authentication flags to the given flag set.
func (o *AuthOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ClientID, "auth.client-id", o.ClientID, "Verda API client ID")
	fs.StringVar(&o.ClientSecret, "auth.client-secret", o.ClientSecret, "Verda API client secret")
	fs.StringVar(&o.BearerToken, "auth.token", o.BearerToken, "Bearer token to send with API requests")
	fs.StringVar(&o.Profile, "auth.profile", o.Profile, "Shared credentials profile to use from ~/.verda/credentials")
	fs.StringVar(&o.CredentialsFile, "auth.credentials-file", o.CredentialsFile, "Path to a shared credentials file in AWS-style INI format")
}

// Complete fills in zero-value fields from viper (config file / env).
func (o *Options) Complete() {
	if o.Config == "" {
		o.Config = viper.GetString(FlagConfig)
	}
	if o.Server == "" {
		o.Server = viper.GetString("server")
	}
	if o.Timeout == 0 {
		o.Timeout = viper.GetDuration("timeout")
	}
	a := o.AuthOptions
	if a.ClientID == "" {
		a.ClientID = viper.GetString("auth.client-id")
	}
	if a.ClientID == "" {
		a.ClientID = os.Getenv("VERDA_CLIENT_ID")
	}
	if a.ClientSecret == "" {
		a.ClientSecret = viper.GetString("auth.client-secret")
	}
	if a.ClientSecret == "" {
		a.ClientSecret = os.Getenv("VERDA_CLIENT_SECRET")
	}
	if a.BearerToken == "" {
		a.BearerToken = viper.GetString("auth.token")
	}
	if a.Profile == "" {
		a.Profile = viper.GetString("auth.profile")
	}
	if a.Profile == "" {
		a.Profile = os.Getenv("VERDA_PROFILE")
	}
	if a.Profile == "" {
		a.Profile = defaultCredentialsProfile
	}
	if a.CredentialsFile == "" {
		a.CredentialsFile = viper.GetString("auth.credentials-file")
	}
	if a.CredentialsFile == "" {
		a.CredentialsFile = os.Getenv("VERDA_SHARED_CREDENTIALS_FILE")
	}
	if a.CredentialsFile == "" {
		if home, err := os.UserHomeDir(); err == nil {
			a.CredentialsFile = filepath.Join(home, defaultCredentialsPath)
		}
	}

	missingRequired := a.ClientID == "" || a.ClientSecret == ""
	if missingRequired || a.BearerToken == "" {
		shared, err := loadSharedCredentials(a.CredentialsFile, a.Profile)
		switch {
		case err == nil:
			if a.ClientID == "" {
				a.ClientID = shared.ClientID
			}
			if a.ClientSecret == "" {
				a.ClientSecret = shared.ClientSecret
			}
			if a.BearerToken == "" {
				a.BearerToken = shared.BearerToken
			}
		case missingRequired && !os.IsNotExist(err):
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
		return fmt.Errorf("--server must not be empty")
	}
	if o.Timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	return nil
}
