package doctor

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	clioptions "github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// checkResult holds the outcome of a single diagnostic check.
type checkResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "ok", "warn", "fail", "skip"
	Detail string `json:"detail,omitempty"`
}

// report is the structured output for the doctor command.
type report struct {
	Checks []checkResult `json:"checks"`
}

// NewCmdDoctor creates the doctor diagnostic command.
func NewCmdDoctor(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common issues",
		Long: cmdutil.LongDesc(`
			Run a series of diagnostic checks against your Verda CLI
			installation and report any issues found. Checks include
			credential configuration, API reachability, authentication,
			CLI version, binary location, and directory permissions.
		`),
		Example: cmdutil.Examples(`
			# Run all diagnostic checks
			verda doctor

			# Output as JSON for scripting
			verda doctor -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Best-effort credential resolution so doctor works even
			// when credentials are bad or missing.
			f.Options().Complete()
			return runDoctor(cmd, f, ioStreams)
		},
	}
}

func runDoctor(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	ctx := cmd.Context()

	// 1. Credentials found
	credResult := checkCredentials(f)

	// 2. API reachable
	apiResult := checkAPIReachable(ctx, f)

	// 3. Authentication valid (skip if creds or API failed)
	authResult := checkAuthentication(f, credResult, apiResult)

	checks := []checkResult{
		credResult,
		apiResult,
		authResult,
		checkCLIVersion(ctx),   // 4. CLI up to date
		checkBinaryInstalled(), // 5. Binary installed
		checkTemplatesDir(),    // 6. Templates directory
		checkConfigDir(),       // 7. Config directory
	}

	r := report{Checks: checks}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Doctor report:", r)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), r); wrote {
		return err
	}

	// Human-readable table output.
	for _, c := range checks {
		symbol := statusSymbol(c.Status)
		detail := ""
		if c.Detail != "" {
			detail = " (" + c.Detail + ")"
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s %s%s\n", symbol, c.Name, detail)
	}

	return nil
}

// checkCredentials verifies that a credentials file exists and contains keys.
func checkCredentials(f cmdutil.Factory) checkResult {
	name := "Credentials found"

	credFile := f.Options().AuthOptions.CredentialsFile
	if credFile == "" {
		var err error
		credFile, err = clioptions.DefaultCredentialsFilePath()
		if err != nil {
			return checkResult{Name: name, Status: "fail", Detail: err.Error()}
		}
	}

	info, err := os.Stat(credFile)
	if err != nil {
		if os.IsNotExist(err) {
			return checkResult{Name: name, Status: "fail", Detail: shortPath(credFile) + " not found"}
		}
		return checkResult{Name: name, Status: "fail", Detail: err.Error()}
	}
	if info.IsDir() {
		return checkResult{Name: name, Status: "fail", Detail: shortPath(credFile) + " is a directory"}
	}

	// File exists — check if keys are configured.
	auth := f.Options().AuthOptions
	if auth.ClientID == "" || auth.ClientSecret == "" {
		return checkResult{Name: name, Status: "warn", Detail: shortPath(credFile) + " exists but credentials are missing or incomplete"}
	}

	return checkResult{Name: name, Status: "ok", Detail: shortPath(credFile)}
}

// checkAPIReachable sends a HEAD request to the API server.
func checkAPIReachable(ctx context.Context, f cmdutil.Factory) checkResult {
	name := "API reachable"
	server := f.Options().Server

	ctx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, server, http.NoBody)
	if err != nil {
		return checkResult{Name: name, Status: "fail", Detail: err.Error()}
	}

	resp, err := f.HTTPClient().Do(req)
	if err != nil {
		return checkResult{Name: name, Status: "fail", Detail: err.Error()}
	}
	_ = resp.Body.Close()

	// Any HTTP response (even 4xx) means the server is reachable.
	return checkResult{Name: name, Status: "ok", Detail: server}
}

// checkAuthentication verifies that f.Token() returns a valid token.
func checkAuthentication(f cmdutil.Factory, cred, api checkResult) checkResult {
	name := "Authentication valid"

	if cred.Status != "ok" || api.Status != "ok" {
		return checkResult{Name: name, Status: "skip", Detail: "skipped"}
	}

	token := f.Token()
	if token == "" {
		return checkResult{Name: name, Status: "fail", Detail: "could not obtain token"}
	}

	return checkResult{Name: name, Status: "ok"}
}

// checkCLIVersion compares the current version against the latest release.
func checkCLIVersion(ctx context.Context) checkResult {
	name := "CLI up to date"

	latest, current, err := cmdutil.CheckVersion(ctx)
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	if cmdutil.CompareVersions(latest, current) > 0 {
		return checkResult{Name: name, Status: "warn", Detail: fmt.Sprintf("%s \u2192 %s available", current, latest)}
	}

	return checkResult{Name: name, Status: "ok", Detail: current}
}

// checkBinaryInstalled verifies the binary is in the recommended directory.
func checkBinaryInstalled() checkResult {
	name := "Binary installed"

	exe, err := os.Executable()
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	binDir, err := clioptions.VerdaBinDir()
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	exeDir := filepath.Dir(exe)
	if exeDir == binDir {
		return checkResult{Name: name, Status: "ok", Detail: shortPath(exe)}
	}

	return checkResult{Name: name, Status: "warn", Detail: fmt.Sprintf("running from %s, recommended: %s", shortPath(exe), shortPath(binDir))}
}

// checkTemplatesDir checks the templates directory existence and permissions.
func checkTemplatesDir() checkResult {
	name := "Templates directory"

	dir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	return checkDirPerms(name, dir)
}

// checkConfigDir checks the config directory existence and permissions.
func checkConfigDir() checkResult {
	name := "Config directory"

	dir, err := clioptions.VerdaDir()
	if err != nil {
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	return checkDirPerms(name, dir)
}

// checkDirPerms checks that a directory exists and has secure permissions.
// If the directory doesn't exist, it returns ok (not an error — it may not
// have been created yet). On Windows, permission checks are skipped.
func checkDirPerms(name, dir string) checkResult {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return checkResult{Name: name, Status: "ok", Detail: shortPath(dir) + " (not created yet)"}
		}
		return checkResult{Name: name, Status: "warn", Detail: err.Error()}
	}

	if runtime.GOOS == "windows" {
		return checkResult{Name: name, Status: "ok", Detail: shortPath(dir)}
	}

	if !info.IsDir() {
		return checkResult{Name: name, Status: "warn", Detail: shortPath(dir) + " is not a directory"}
	}

	if hasLoosePerms(info) {
		return checkResult{
			Name:   name,
			Status: "warn",
			Detail: fmt.Sprintf("%s has permissions %s, recommended: 0700", shortPath(dir), info.Mode().Perm()),
		}
	}

	return checkResult{Name: name, Status: "ok", Detail: shortPath(dir)}
}

// hasLoosePerms reports whether group or other permission bits are set.
func hasLoosePerms(info fs.FileInfo) bool {
	return info.Mode().Perm()&0o077 != 0
}

// statusSymbol returns a human-readable status indicator.
func statusSymbol(status string) string {
	switch status {
	case "ok":
		return "\u2713" // ✓
	case "warn":
		return "!"
	case "fail":
		return "\u2717" // ✗
	case "skip":
		return "-"
	default:
		return "?"
	}
}

// shortPath replaces the user's home directory prefix with ~.
func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != "" && rel[0] != '.' {
		return "~/" + rel
	}
	return p
}
