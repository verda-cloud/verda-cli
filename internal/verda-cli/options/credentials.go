package options

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

type SharedCredentials struct {
	ClientID     string
	ClientSecret string
	BearerToken  string
}

// DefaultCredentialsFilePath returns the default shared credentials path.
func DefaultCredentialsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultCredentialsPath), nil
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
		ClientID:     strings.TrimSpace(section.Key("client_id").String()),
		ClientSecret: strings.TrimSpace(section.Key("client_secret").String()),
	}

	switch {
	case section.HasKey("token"):
		creds.BearerToken = strings.TrimSpace(section.Key("token").String())
	case section.HasKey("bearer_token"):
		creds.BearerToken = strings.TrimSpace(section.Key("bearer_token").String())
	case section.HasKey("auth_token"):
		creds.BearerToken = strings.TrimSpace(section.Key("auth_token").String())
	}

	return creds, nil
}
