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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/version"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	repo        = "verda-cloud/verda-cli"
	apiBase     = "https://api.github.com"
	httpTimeout = 60 * time.Second
	osWindows   = "windows"
)

// NewCmdUpdate creates the update command.
func NewCmdUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var targetVersion string
	var listVersions bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Verda CLI to the latest or a specific version",
		Long: cmdutil.LongDesc(`
			Update the Verda CLI binary in-place by downloading from GitHub Releases.
			No Go installation required.

			Without flags, updates to the latest version.
			Use --version to install a specific version (upgrade or downgrade).
			Use --list to show available versions.
		`),
		Example: cmdutil.Examples(`
			# Update to latest
			verda update

			# Install specific version
			verda update --version v1.0.0

			# List available versions
			verda update --list
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listVersions {
				return runList(cmd.Context(), ioStreams)
			}
			return runUpdate(cmd.Context(), f, ioStreams, targetVersion)
		},
	}

	cmd.Flags().StringVar(&targetVersion, "version", "", "Version to install (e.g. v1.0.0)")
	cmd.Flags().BoolVar(&listVersions, "list", false, "List available versions")

	return cmd
}

func runList(ctx context.Context, ioStreams cmdutil.IOStreams) error {
	versions, err := fetchVersions(ctx)
	if err != nil {
		return err
	}

	current := version.Get().GitVersion
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

func runUpdate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, targetVersion string) error {
	current := version.Get().GitVersion

	// Resolve target version.
	target := targetVersion
	if target == "" {
		latest, err := fetchLatestVersion(ctx)
		if err != nil {
			return err
		}
		target = latest
	}
	if !strings.HasPrefix(target, "v") {
		target = "v" + target
	}

	if target == current {
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

	// Download.
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Downloading %s...", target))
	}
	binary, err := downloadRelease(ctx, target)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	// Replace current executable.
	exe, err := resolveExecutable()
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	if err := replaceBinary(exe, binary); err != nil {
		if errors.Is(err, os.ErrPermission) {
			hint := "sudo verda update"
			if runtime.GOOS == osWindows {
				hint = "running the command in an elevated (Administrator) terminal"
			}
			return fmt.Errorf("permission denied writing to %s\n\nTry: %s", filepath.Dir(exe), hint)
		}
		return fmt.Errorf("replacing binary: %w", err)
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Updated to %s\n", target)
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

func downloadRelease(ctx context.Context, tag string) ([]byte, error) {
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
		ext = "zip"
	}
	assetName := fmt.Sprintf("verda_%s_%s_%s.%s", versionNum, runtime.GOOS, runtime.GOARCH, ext)

	var downloadURL string
	for i := range rel.Assets {
		if rel.Assets[i].Name == assetName {
			downloadURL = rel.Assets[i].BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return nil, fmt.Errorf("no asset %q found in release %s", assetName, tag)
	}

	// Download the archive.
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
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

	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Extract the binary from the archive.
	binaryName := "verda"
	if runtime.GOOS == osWindows {
		binaryName = "verda.exe"
	}

	if ext == "zip" {
		return extractFromZip(archiveData, binaryName)
	}
	return extractFromTarGz(archiveData, binaryName)
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

// --- Binary replacement ---

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
