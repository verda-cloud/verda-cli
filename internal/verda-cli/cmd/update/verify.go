package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// VerifyResult holds the outcome of a binary verification check.
type VerifyResult struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	Match        bool   `json:"match"`
	ExpectedHash string `json:"expectedHash"`
	ActualHash   string `json:"actualHash"`
}

// checksumURL returns the GitHub release URL for the checksums file.
func checksumURL(ver string) string {
	bare := strings.TrimPrefix(ver, "v")
	tag := "v" + bare // GitHub release tags always have v prefix
	return fmt.Sprintf(
		"https://github.com/verda-cloud/verda-cli/releases/download/%s/verda_%s_binary_SHA256SUMS",
		tag, bare,
	)
}

// fetchChecksums downloads the checksum file from the given URL.
func fetchChecksums(client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching checksums: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on read path

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching checksums: HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return "", fmt.Errorf("reading checksums: %w", err)
	}
	return string(body), nil
}

// hashFile computes the SHA256 hex digest of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is from os.Executable, not user input
	if err != nil {
		return "", fmt.Errorf("opening binary: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on read path

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing binary: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseChecksumLine parses a line like "<hash>  <key>" into its parts.
// Returns false if the line is empty, a comment, or malformed.
func parseChecksumLine(line string) (hexStr, key string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	// Format: "<hash>  <path>" (two spaces is conventional, but split on any whitespace)
	parts := strings.Fields(line)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// findMatchingChecksum finds the expected hash for the given OS/arch from the
// checksum file body.
func findMatchingChecksum(body, goos, goarch string) (string, error) {
	// GoReleaser dist directories include arch variant suffixes, e.g.:
	//   verda_darwin_arm64_v8.0/verda
	//   verda_linux_amd64_v1/verda
	// Match "verda_<os>_<arch>" followed by "/" or "_" (variant suffix).
	prefix := fmt.Sprintf("verda_%s_%s", goos, goarch)

	for _, line := range strings.Split(body, "\n") {
		hexStr, key, ok := parseChecksumLine(line)
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		// After the os_arch prefix, expect "/" or "_" (variant like _v8.0/)
		rest := key[len(prefix):]
		if rest != "" && (rest[0] == '/' || rest[0] == '_') {
			return hexStr, nil
		}
	}
	return "", fmt.Errorf("no checksum entry found for %s/%s", goos, goarch)
}

// verifyBinary fetches the checksums and compares against the binary at binPath.
func verifyBinary(client *http.Client, binPath, url, goos, goarch string) (*VerifyResult, error) {
	body, err := fetchChecksums(client, url)
	if err != nil {
		return nil, err
	}

	expected, err := findMatchingChecksum(body, goos, goarch)
	if err != nil {
		return nil, err
	}

	actual, err := hashFile(binPath)
	if err != nil {
		return nil, err
	}

	return &VerifyResult{
		Platform:     fmt.Sprintf("%s/%s", goos, goarch),
		Match:        actual == expected,
		ExpectedHash: expected,
		ActualHash:   actual,
	}, nil
}

// runVerify is the top-level verify logic, writing output to out and warnings
// to errOut. It uses the provided HTTP client for fetching checksums.
func runVerify(out, errOut io.Writer, outputFormat string, client *http.Client, ver, goos, goarch string) error {
	bare := strings.TrimPrefix(ver, "v")
	if bare == "0.0.0-dev" || bare == "" {
		_, _ = fmt.Fprintf(errOut, "Warning: cannot verify a development build (%s)\n", ver)
		return errors.New("cannot verify development build")
	}

	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating binary: %w", err)
	}
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolving binary path: %w", err)
	}

	url := checksumURL(ver)
	result, err := verifyBinary(client, binPath, url, goos, goarch)
	if err != nil {
		return err
	}
	result.Version = ver

	if wrote, werr := cmdutil.WriteStructured(out, outputFormat, result); wrote {
		if !result.Match {
			if werr != nil {
				return werr
			}
			return errors.New("checksum mismatch")
		}
		return werr
	}

	if result.Match {
		_, _ = fmt.Fprintf(out, "Verification successful: binary matches release checksum.\n")
		_, _ = fmt.Fprintf(out, "  Version:  %s\n  Platform: %s\n  SHA256:   %s\n",
			result.Version, result.Platform, result.ActualHash)
	} else {
		_, _ = fmt.Fprintf(errOut, "Verification FAILED: binary does NOT match release checksum!\n")
		_, _ = fmt.Fprintf(errOut, "  Version:  %s\n  Platform: %s\n  Expected: %s\n  Actual:   %s\n",
			result.Version, result.Platform, result.ExpectedHash, result.ActualHash)
		return errors.New("checksum mismatch")
	}

	return nil
}
