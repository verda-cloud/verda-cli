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
