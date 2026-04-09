package util

import (
	"path/filepath"

	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

// TemplatesBaseDir returns the base directory for template storage (~/.verda/templates).
func TemplatesBaseDir() (string, error) {
	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(verdaDir, "templates"), nil
}
