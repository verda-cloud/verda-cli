package options

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const verdaDirName = ".verda"

// VerdaDir returns the path to the Verda configuration directory.
//
// Resolution order:
//  1. VERDA_HOME environment variable (if set)
//  2. Platform default:
//     - macOS/Linux: ~/.verda
//     - Windows:     %USERPROFILE%\.verda
func VerdaDir() (string, error) {
	if dir := os.Getenv("VERDA_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w\n\n"+
			"Set the VERDA_HOME environment variable to specify the config directory", err)
	}
	return filepath.Join(home, verdaDirName), nil
}

// EnsureVerdaDir creates the Verda config directory if it doesn't exist,
// with restrictive permissions (0700 on Unix, default ACL on Windows).
func EnsureVerdaDir() (string, error) {
	dir, err := VerdaDir()
	if err != nil {
		return "", err
	}
	if err := mkdirSecure(dir); err != nil {
		return "", fmt.Errorf("cannot create config directory %s: %w\n\n"+
			"Check directory permissions or set VERDA_HOME to an alternative location", dir, err)
	}
	return dir, nil
}

// WriteSecureFile writes data to path with restrictive permissions.
// On Unix, the file is created with 0600. On Windows, it inherits the
// parent directory ACL (Go's os.WriteFile uses default security).
func WriteSecureFile(path string, data []byte) error {
	if err := mkdirSecure(filepath.Dir(path)); err != nil {
		return fmt.Errorf("cannot create directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// On Unix, enforce 0600 even if umask was permissive.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
	}
	return nil
}

// mkdirSecure creates a directory with 0700 on Unix.
func mkdirSecure(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// On Unix, enforce 0700 on the leaf directory.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, 0o700) //nolint:gosec // 0700 is correct for a config directory
	}
	return nil
}
