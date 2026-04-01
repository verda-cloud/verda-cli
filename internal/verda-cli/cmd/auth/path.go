package auth

import (
	"os"
	"path/filepath"

	"github/verda-cloud/verda-cli/internal/verda-cli/options"
)

func defaultConfigFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".verda", "config.yaml"), nil
}

func resolveCredentialsFile(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	if path = os.Getenv("VERDA_SHARED_CREDENTIALS_FILE"); path != "" {
		return path, nil
	}
	return options.DefaultCredentialsFilePath()
}
