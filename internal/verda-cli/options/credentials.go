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

// ListProfiles returns the names of all profiles in the credentials file.
// It returns an empty slice (not an error) if the file does not exist.
func ListProfiles(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}

	cfg, err := ini.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []string
	for _, s := range cfg.Sections() {
		name := s.Name()
		if name == "DEFAULT" {
			continue // ini.v1 always has a synthetic DEFAULT section
		}
		profiles = append(profiles, name)
	}
	return profiles, nil
}

// LoadSharedCredentialsForProfile loads credentials for a specific profile.
func LoadSharedCredentialsForProfile(path, profile string) (*SharedCredentials, error) {
	return loadSharedCredentials(path, profile)
}

func loadSharedCredentials(path, profile string) (*SharedCredentials, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}

	cfg, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	section, err := cfg.GetSection(profile)
	if err != nil {
		return nil, profileNotFoundError(cfg, profile, path)
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

// profileNotFoundError builds a helpful error listing available profiles.
func profileNotFoundError(cfg *ini.File, profile, path string) error {
	var available []string
	for _, s := range cfg.Sections() {
		if s.Name() != "DEFAULT" {
			available = append(available, s.Name())
		}
	}

	msg := fmt.Sprintf("credentials profile %q not found in %s", profile, path)
	if len(available) > 0 {
		msg += "\n  available profiles: " + strings.Join(available, ", ")
		msg += "\n  run 'verda auth use' to switch profile"
	} else {
		msg += "\n  no profiles found — run 'verda auth login' to set up credentials"
	}
	return fmt.Errorf("%s", msg)
}
