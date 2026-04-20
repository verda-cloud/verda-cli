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

package registry

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// loginOptions bundles the flag state for `verda registry login`.
type loginOptions struct {
	Profile         string
	CredentialsFile string
	DockerConfig    string
}

// NewCmdLogin creates the `verda registry login` command.
//
// login writes the active registry credentials into ~/.docker/config.json
// so third-party tools (docker pull, docker-compose, helm pull oci://,
// nerdctl, ...) can authenticate against vccr.io. It does NOT talk to the
// registry — it is a pure file-merge. `configure` is for storing Verda's
// own credentials; `login` is for interop.
func NewCmdLogin(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &loginOptions{
		Profile: defaultProfileName,
	}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Write VCR credentials into ~/.docker/config.json",
		Long: cmdutil.LongDesc(`
			Write the active Verda Container Registry credentials into the Docker
			config file so third-party tools (docker pull, docker-compose,
			helm pull oci://, nerdctl) can authenticate against the registry.

			This command does NOT talk to the registry; it is a local file merge.
			Existing entries for other registries, as well as unknown top-level
			keys (credsStore, credHelpers, HttpHeaders, ...), are preserved
			verbatim.

			Default Docker config path: ~/.docker/config.json
			Override with --config or DOCKER_CONFIG.
		`),
		Example: cmdutil.Examples(`
			# Write the default profile into ~/.docker/config.json
			verda registry login

			# Use a specific profile
			verda registry login --profile staging

			# Write to a non-default Docker config file
			verda registry login --config /tmp/docker/config.json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "Credentials profile to read")
	flags.StringVar(&opts.CredentialsFile, "credentials-file", "", "Path to the shared Verda credentials file")
	flags.StringVar(&opts.DockerConfig, "config", "", "Path to the Docker config file (default ~/.docker/config.json)")

	return cmd
}

// runLogin is the RunE body, split out for testability.
func runLogin(f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *loginOptions) error {
	creds, err := loadCredsFromFactory(f, opts.Profile, opts.CredentialsFile)
	if err != nil {
		// Missing credentials file / profile section manifest as an error with
		// nil creds here. Treat every "no usable creds" shape identically:
		// checkExpiry(nil) yields the structured registry_not_configured error.
		// If a caller hits a genuinely different error (e.g. malformed INI)
		// we still fall through to the same message — callers should run
		// `verda registry show` to diagnose, and that path reads the file
		// directly. Keeping a single error surface here avoids leaking I/O
		// details into the agent-mode envelope.
		_ = err
		return checkExpiry(nil)
	}

	if err := checkExpiry(creds); err != nil {
		return err
	}

	dockerPath, err := resolveDockerConfigPath(opts.DockerConfig)
	if err != nil {
		return err
	}

	if err := writeDockerLogin(dockerPath, creds); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out,
		"Logged in to %s. Credentials written to %s for use with docker pull / docker-compose / helm.\n",
		creds.Endpoint, dockerPath)
	return nil
}

// resolveDockerConfigPath decides where to read/write the Docker config file.
//
// Resolution order:
//  1. explicit --config flag
//  2. $DOCKER_CONFIG/config.json
//  3. $HOME/.docker/config.json
func resolveDockerConfigPath(flagOverride string) (string, error) {
	if flagOverride != "" {
		return flagOverride, nil
	}
	if env := os.Getenv("DOCKER_CONFIG"); env != "" {
		return filepath.Join(env, "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".docker", "config.json"), nil
}

// dockerAuthEntry is the per-registry record inside ~/.docker/config.json's
// "auths" object. We emit only "auth" (base64 of "user:secret"); legacy
// plaintext fields present in an existing file are dropped on update.
type dockerAuthEntry struct {
	Auth string `json:"auth,omitempty"`
}

// writeDockerLogin reads an existing Docker config (if any), merges the
// auth entry for creds.Endpoint, preserves all other top-level keys and
// other registries' entries verbatim, and writes atomically.
func writeDockerLogin(path string, creds *options.RegistryCredentials) error {
	top, err := readDockerConfig(path)
	if err != nil {
		return err
	}

	auths, err := extractAuths(top)
	if err != nil {
		return err
	}

	// Compute the merged entry. If a prior record exists we only keep `auth`;
	// legacy `username`/`password`/`identitytoken`/`registrytoken` are
	// dropped to prevent stale fields from conflicting with the base64 we
	// just wrote (Docker's own login does the same).
	entry := dockerAuthEntry{
		Auth: base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Secret)),
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal auth entry: %w", err)
	}
	auths[creds.Endpoint] = json.RawMessage(encoded)

	authsEncoded, err := json.Marshal(auths)
	if err != nil {
		return fmt.Errorf("marshal auths: %w", err)
	}
	top["auths"] = json.RawMessage(authsEncoded)

	// Docker's own tooling pretty-prints config.json with tab indentation;
	// match it so diffs stay readable.
	out, err := json.MarshalIndent(top, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal docker config: %w", err)
	}

	return atomicWrite(path, out)
}

// readDockerConfig loads the existing Docker config as a generic map so
// we can preserve every top-level key we don't explicitly manage
// (credsStore, credHelpers, HttpHeaders, psFormat, ...). Missing file is
// not an error — it just yields an empty map.
func readDockerConfig(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		return nil, fmt.Errorf("read docker config %q: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]json.RawMessage{}, nil
	}

	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse docker config %q: %w", path, err)
	}
	return top, nil
}

// extractAuths returns the mutable "auths" sub-object from the top-level
// config map, decoding it if present. An absent or null "auths" entry
// yields an empty map, ready to be populated.
func extractAuths(top map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw, ok := top["auths"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	auths := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &auths); err != nil {
		return nil, fmt.Errorf("parse docker config auths: %w", err)
	}
	return auths, nil
}

// atomicWrite writes data to a sibling `.new` file, chmods 0600 (POSIX
// only), then renames over the final path. On Windows we skip chmod since
// the POSIX bits are not meaningful and rename semantics differ, but we
// still write via a temporary file for crash safety.
func atomicWrite(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create parent directory %q: %w", dir, err)
		}
	}

	tmp := path + ".new"
	// Write with restrictive perms; credentials are auth material.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", tmp, err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o600); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("chmod %q: %w", tmp, err)
		}
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %q -> %q: %w", tmp, path, err)
	}
	return nil
}
