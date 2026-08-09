// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/version"

	skillscmd "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/skills"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

const (
	repo        = "verda-cloud/verda-cli"
	httpTimeout = 60 * time.Second
	osWindows   = "windows"
	zipExt      = "zip"

	binNameUnix = "verda"
	binNameWin  = "verda.exe"
)

// apiBase is a var so tests can point the GitHub client at a fixture server.
var apiBase = "https://api.github.com"

// platformBinaryName returns the installed binary name for the current OS.
func platformBinaryName() string {
	if runtime.GOOS == osWindows {
		return binNameWin
	}
	return binNameUnix
}

// NewCmdUpdate creates the update command.
func NewCmdUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var targetVersion string
	var listVersions bool
	var verify bool
	var skipVerify bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Verda CLI to the latest or a specific version",
		Long: cmdutil.LongDesc(`
			Update the Verda CLI binary in-place by downloading from GitHub Releases.
			No Go installation required.

			The binary is installed to ~/.verda/bin/ (no sudo required).

			The downloaded archive is verified against the release's SHA256SUMS
			before the running binary is replaced; on any verification failure
			the update aborts and the binary is left untouched. Use --skip-verify
			only as a deliberate escape hatch.

			Without flags, updates to the latest version.
			Use --target to install a specific version (upgrade or downgrade).
			Use --list to show available versions.
		`),
		Example: cmdutil.Examples(`
			# Update to latest
			verda update

			# Install specific version
			verda update --target v1.0.0

			# List available versions
			verda update --list
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listVersions {
				return runList(cmd.Context(), f, ioStreams)
			}
			if verify {
				info := version.Get()
				verifyCtx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
				defer cancel()
				return runVerify(verifyCtx, ioStreams.Out, ioStreams.ErrOut, f.OutputFormat(), f.HTTPClient(), info.GitVersion, runtime.GOOS, runtime.GOARCH)
			}
			return runUpdate(cmd.Context(), f, ioStreams, targetVersion, skipVerify)
		},
	}

	cmd.Flags().StringVar(&targetVersion, "target", "", "Version to install (e.g. v1.0.0)")
	cmd.Flags().BoolVar(&listVersions, "list", false, "List available versions")
	cmd.Flags().BoolVar(&verify, "verify", false, "Verify the binary checksum against the GitHub release")
	cmd.Flags().BoolVar(&skipVerify, "skip-verify", false, "Skip checksum verification of the downloaded release archive (NOT recommended)")

	return cmd
}

func runList(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	versions, err := cmdutil.WithSpinner(ctx, f.Status(), "Fetching available versions...", fetchVersions)
	if err != nil {
		return err
	}

	current := version.Get().GitVersion
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "  Available versions (current: %s)\n\n", current)
	for _, v := range versions {
		marker := "  "
		if v == current {
			marker = "* "
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s%s\n", marker, v)
	}
	return nil
}

// updateResult is the machine-readable (-o json / --agent) update outcome.
type updateResult struct {
	Version          string `json:"version"`
	PreviousVersion  string `json:"previousVersion"`
	Path             string `json:"path,omitempty"`
	Updated          bool   `json:"updated"`
	ChecksumVerified bool   `json:"checksumVerified"`
}

func runUpdate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, targetVersion string, skipVerify bool) error {
	format := f.OutputFormat()
	current := version.Get().GitVersion
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}

	// Resolve target version.
	target := targetVersion
	if target == "" {
		latest, err := cmdutil.WithSpinner(ctx, f.Status(), "Checking for latest version...", fetchLatestVersion)
		if err != nil {
			return err
		}
		target = latest
	}
	if !strings.HasPrefix(target, "v") {
		target = "v" + target
	}

	if target == current {
		res := updateResult{Version: current, PreviousVersion: current}
		if wrote, err := cmdutil.WriteStructured(ioStreams.Out, format, res); wrote {
			return err
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "Already at %s\n", current)
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Updating %s -> %s\n", current, target)

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Update:", map[string]string{
		"current": current,
		"target":  target,
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	})

	// Download (checksum-verified unless --skip-verify).
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Downloading %s...", target))
	}
	binary, err := downloadRelease(ctx, target, skipVerify)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	// Determine install destination: always ~/.verda/bin/verda.
	binDir, err := options.EnsureVerdaBinDir()
	if err != nil {
		return fmt.Errorf("preparing install directory: %w", err)
	}
	dst := filepath.Join(binDir, platformBinaryName())

	if err := replaceBinary(dst, binary); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	res := updateResult{
		Version:          target,
		PreviousVersion:  current,
		Path:             dst,
		Updated:          true,
		ChecksumVerified: !skipVerify,
	}
	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, format, res); wrote {
		if werr != nil {
			return werr
		}
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "Updated to %s\n", target)
	}

	// Update installed skills if any agents have them.
	updateInstalledSkills(ctx, dst, ioStreams)

	// Migrate: if the currently running binary is outside ~/.verda/bin/,
	// handle the old location based on how it was installed.
	oldExe, _ := executablePath()
	if oldExe != "" && oldExe != dst {
		if isManagedByPackageManager(oldExe) {
			// Installed via Homebrew, apt, rpm, etc. — don't touch it.
			// Let the package manager handle upgrades for that path.
			_, _ = fmt.Fprintf(ioStreams.ErrOut,
				"\nNote: %s appears to be managed by a package manager.\n"+
					"  Future updates via 'verda update' will install to %s.\n"+
					"  Use your package manager to update or remove the old binary.\n",
				oldExe, dst)
		} else {
			// Manual install (curl, manual download) — safe to replace in-place.
			if err := replaceBinary(oldExe, binary); err != nil {
				_, _ = fmt.Fprintf(ioStreams.ErrOut,
					"\nWarning: could not update old binary at %s: %v\n", oldExe, err)
			}

			_, _ = fmt.Fprintf(ioStreams.ErrOut,
				"\nNote: verda is now installed at %s\n"+
					"  Add it to your PATH:  export PATH=\"%s:$PATH\"\n"+
					"  Then remove the old binary:  sudo rm %s\n",
				dst, binDir, oldExe)
		}
	}

	return nil
}

// --- GitHub API ---

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func fetchLatestVersion(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)
	var rel ghRelease
	if err := ghGet(ctx, url, &rel); err != nil {
		return "", fmt.Errorf("checking latest version: %w", err)
	}
	if rel.TagName == "" {
		return "", errors.New("no releases found")
	}
	return rel.TagName, nil
}

func fetchVersions(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=20", apiBase, repo)
	var releases []ghRelease
	if err := ghGet(ctx, url, &releases); err != nil {
		return nil, fmt.Errorf("listing versions: %w", err)
	}
	versions := make([]string, 0, len(releases))
	for i := range releases {
		if releases[i].TagName != "" {
			versions = append(versions, releases[i].TagName)
		}
	}
	return versions, nil
}

func downloadRelease(ctx context.Context, tag string, skipVerify bool) ([]byte, error) {
	// Fetch release to get asset URLs.
	url := fmt.Sprintf("%s/repos/%s/releases/tags/%s", apiBase, repo, tag)
	var rel ghRelease
	if err := ghGet(ctx, url, &rel); err != nil {
		return nil, fmt.Errorf("fetching release %s: %w", tag, err)
	}

	// Find the right asset.
	versionNum := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if runtime.GOOS == osWindows {
		ext = zipExt
	}
	assetName := fmt.Sprintf("verda_%s_%s_%s.%s", versionNum, runtime.GOOS, runtime.GOARCH, ext)

	downloadURL := findAssetURL(&rel, assetName)
	if downloadURL == "" {
		return nil, fmt.Errorf("no asset %q found in release %s", assetName, tag)
	}

	// Download the archive.
	client := &http.Client{Timeout: httpTimeout}
	archiveData, err := downloadAsset(ctx, client, downloadURL)
	if err != nil {
		return nil, err
	}

	// Integrity gate: verify the archive bytes against the release's archive
	// sums (goreleaser checksum pipe output verda_<VER>_SHA256SUMS) before
	// anything is extracted or replaced. Fails closed unless --skip-verify.
	if !skipVerify {
		if err := verifyReleaseArchive(ctx, client, &rel, archiveData, assetName, versionNum); err != nil {
			return nil, err
		}
	}

	// Extract the binary from the archive.
	binaryName := platformBinaryName()
	if ext == zipExt {
		return extractFromZip(archiveData, binaryName)
	}
	return extractFromTarGz(archiveData, binaryName)
}

// findAssetURL returns the browser download URL of the named release asset.
func findAssetURL(rel *ghRelease, name string) string {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return rel.Assets[i].BrowserDownloadURL
		}
	}
	return ""
}

func downloadAsset(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// verifyReleaseArchive checks the downloaded archive against the release's
// archive sums file. Any fetch/parse/mismatch failure aborts the update.
func verifyReleaseArchive(ctx context.Context, client *http.Client, rel *ghRelease, archiveData []byte, assetName, versionNum string) error {
	sumsName := fmt.Sprintf("verda_%s_SHA256SUMS", versionNum)
	sumsURL := findAssetURL(rel, sumsName)
	if sumsURL == "" {
		return checksumAbort(fmt.Errorf("checksum file %q not found in release assets", sumsName))
	}
	sumsBody, err := fetchChecksums(ctx, client, sumsURL)
	if err != nil {
		return checksumAbort(err)
	}
	if err := verifyArchiveChecksum(archiveData, sumsBody, assetName); err != nil {
		return checksumAbort(err)
	}
	return nil
}

// checksumAbort wraps a verification failure so the message names the
// --skip-verify escape hatch and states that the binary was left untouched.
func checksumAbort(err error) error {
	return fmt.Errorf("checksum verification failed: %w; update aborted, binary left untouched (use --skip-verify to bypass)", err)
}

func extractFromTarGz(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close() //nolint:errcheck // best-effort close

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%q not found in archive", name)
}

func extractFromZip(data []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.Name == name {
			return readZipEntry(f)
		}
	}
	return nil, fmt.Errorf("%q not found in archive", name)
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck // best-effort close
	return io.ReadAll(rc)
}

// isManagedByPackageManager returns true if the binary path looks like it was
// installed by a package manager (Homebrew, apt/dpkg, rpm, apk, Scoop).
func isManagedByPackageManager(exePath string) bool {
	managedPrefixes := []string{
		"/opt/homebrew/",     // Homebrew (Apple Silicon)
		"/usr/local/Cellar/", // Homebrew (Intel Mac)
		"/home/linuxbrew/",   // Homebrew (Linux)
		"/usr/bin/",          // apt/dpkg, rpm, apk system packages
		"/snap/",             // Snap packages
	}
	for _, prefix := range managedPrefixes {
		if strings.HasPrefix(exePath, prefix) {
			return true
		}
	}
	// Scoop on Windows: ~/scoop/apps/
	if runtime.GOOS == osWindows && strings.Contains(exePath, `\scoop\apps\`) {
		return true
	}
	return false
}

// --- Binary replacement ---

// executablePath is wrapped so update tests can stub the running-binary path;
// otherwise a happy-path runUpdate test would try to rewrite the test binary.
var executablePath = resolveExecutable

func resolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func replaceBinary(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "verda-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	_ = tmp.Close()

	// On Windows, remove destination before rename.
	if runtime.GOOS == osWindows {
		_ = os.Remove(dst)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// --- HTTP helper ---

func ghGet(ctx context.Context, url string, v any) error {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API: HTTP %d for %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// updateInstalledSkills re-installs skills for agents that already have them.
// It runs the NEW binary (at dst) so the latest embedded skills are used.
// Best-effort: failures are reported as warnings, not errors.
func updateInstalledSkills(ctx context.Context, newBinary string, ioStreams cmdutil.IOStreams) {
	statePath, err := skillscmd.StatePath()
	if err != nil {
		return
	}
	state, err := skillscmd.LoadState(statePath)
	if err != nil || state.Version == "" || len(state.Agents) == 0 {
		return
	}

	args := make([]string, 0, 6+len(state.Agents))
	args = append(args, "--agent", "-o", "json", "skills", "install", "--force")
	args = append(args, state.Agents...)

	cmd := exec.CommandContext(ctx, newBinary, args...) //nolint:gosec // newBinary is the just-installed verda binary
	cmd.Stdout = ioStreams.Out
	cmd.Stderr = ioStreams.ErrOut

	if err := cmd.Run(); err != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not update skills: %v\n", err)
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Run 'verda skills install %s --force' manually.\n",
			strings.Join(state.Agents, " "))
	}
}
