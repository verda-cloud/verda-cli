package options

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

type SharedCredentials struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	BearerToken  string
}

// DefaultCredentialsFilePath returns the default shared credentials path.
func DefaultCredentialsFilePath() (string, error) {
	dir, err := VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// LoadSharedCredentialsForProfile loads credentials for a specific profile.
func LoadSharedCredentialsForProfile(path string, profile string) (*SharedCredentials, error) {
	return loadSharedCredentials(path, profile)
}

func loadSharedCredentials(path string, profile string) (*SharedCredentials, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}

	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	section, err := cfg.GetSection(profile)
	if err != nil {
		return nil, fmt.Errorf("credentials profile %q not found in %s", profile, path)
	}

	creds := &SharedCredentials{
		BaseURL:      strings.TrimSpace(section.Key("verda_base_url").String()),
		ClientID:     strings.TrimSpace(section.Key("verda_client_id").String()),
		ClientSecret: strings.TrimSpace(section.Key("verda_client_secret").String()),
	}

	switch {
	case section.HasKey("verda_token"):
		creds.BearerToken = strings.TrimSpace(section.Key("verda_token").String())
	case section.HasKey("verda_bearer_token"):
		creds.BearerToken = strings.TrimSpace(section.Key("verda_bearer_token").String())
	}

	return creds, nil
}
