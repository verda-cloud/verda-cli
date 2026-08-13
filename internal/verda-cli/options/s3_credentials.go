package options

import (
	"os"
	"strings"

	"gopkg.in/ini.v1"
)

// S3Credentials holds S3 object storage credentials loaded from the
// shared credentials file. These are stored alongside API credentials
// using verda_s3_ prefixed keys.
type S3Credentials struct {
	AccessKey string
	SecretKey string
	Endpoint  string
	Region    string
	AuthMode  string
}

// HasCredentials returns true if the minimum required S3 credentials are set.
func (c *S3Credentials) HasCredentials() bool {
	return c.AccessKey != "" && c.SecretKey != "" && c.Endpoint != ""
}

// LoadS3CredentialsForProfile loads S3 credentials for a specific profile.
func LoadS3CredentialsForProfile(path, profile string) (*S3Credentials, error) {
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

	return &S3Credentials{
		AccessKey: strings.TrimSpace(section.Key("verda_s3_access_key").String()),
		SecretKey: strings.TrimSpace(section.Key("verda_s3_secret_key").String()),
		Endpoint:  strings.TrimSpace(section.Key("verda_s3_endpoint").String()),
		Region:    strings.TrimSpace(section.Key("verda_s3_region").String()),
		AuthMode:  strings.TrimSpace(section.Key("verda_s3_auth_mode").String()),
	}, nil
}

// ApplyS3Env overlays VERDA_S3_* onto c field by field and reports which
// variables were applied (names only — never values; callers print these).
// Empty means unset, so `export VERDA_S3_REGION=` cannot blank a good profile.
func (c *S3Credentials) ApplyS3Env() []string {
	overrides := []struct {
		name  string
		field *string
	}{
		{"VERDA_S3_ACCESS_KEY", &c.AccessKey},
		{"VERDA_S3_SECRET_KEY", &c.SecretKey},
		{"VERDA_S3_ENDPOINT", &c.Endpoint},
		{"VERDA_S3_REGION", &c.Region},
		{"VERDA_S3_AUTH_MODE", &c.AuthMode},
	}

	var applied []string
	for _, o := range overrides {
		if value := strings.TrimSpace(os.Getenv(o.name)); value != "" {
			*o.field = value
			applied = append(applied, o.name)
		}
	}
	return applied
}

// ResolveS3Credentials loads the profile from path, then overlays VERDA_S3_*
// per field: flags (applied later, in the S3 client) → env → file. A missing
// file or profile stops being fatal once env alone carries a complete set —
// that is the CI case, where writing secrets to disk is what we're avoiding.
// The returned credentials are never nil, even alongside an error.
func ResolveS3Credentials(path, profile string) (*S3Credentials, []string, error) {
	creds, err := LoadS3CredentialsForProfile(path, profile)
	if err != nil {
		creds = &S3Credentials{}
	}

	applied := creds.ApplyS3Env()

	if err != nil && !creds.HasCredentials() {
		return creds, applied, err
	}
	return creds, applied, nil
}
