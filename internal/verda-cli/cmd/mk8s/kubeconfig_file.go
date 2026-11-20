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

package mk8s

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

var nonFileNameChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// inputError is a problem with the chosen file, not an I/O failure while writing.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

// asUsage turns an inputError into a usage error (VALIDATION_ERROR, exit 2).
func asUsage(cmd *cobra.Command, err error) error {
	if ie, ok := errors.AsType[*inputError](err); ok {
		return cmdutil.UsageErrorf(cmd, "%s", ie.msg)
	}
	return err
}

func existsMessage(path string) string {
	return path + " already exists; use --force to replace it or --file to choose another path"
}

// kubeconfigTarget: the YAML holds a client private key, so the file is 0600,
// never created over an existing file without force, never written through a symlink.
type kubeconfigTarget struct {
	path string
	// defaultDir: ~/.kube is created 0700 if missing; a custom --file parent must exist.
	defaultDir bool
}

// newKubeconfigTarget: --file empty or equal to the default path gives ~/.kube/verda-<cluster>.<cn>.yaml.
func newKubeconfigTarget(file, cluster, commonName string) (kubeconfigTarget, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		if file != "" {
			return kubeconfigTarget{path: file}, nil
		}
		return kubeconfigTarget{}, fmt.Errorf("find home directory for the default kubeconfig path (use --file): %w", err)
	}
	// '.' cannot come from a sanitized part, so (a-b, c) and (a, b-c) never share
	// a file that --force would overwrite.
	name := "verda-" + sanitizeFileName(cluster) + "." + sanitizeFileName(commonName) + ".yaml"
	def := filepath.Join(home, ".kube", name)
	if file != "" && filepath.Clean(file) != def {
		return kubeconfigTarget{path: file}, nil
	}
	return kubeconfigTarget{path: def, defaultDir: true}, nil
}

func sanitizeFileName(s string) string {
	return nonFileNameChars.ReplaceAllString(s, "-")
}

// Check runs before a certificate is issued: the path is free (or replaceable
// with force) and its directory writable. It leaves nothing behind.
func (t kubeconfigTarget) Check(force bool) error {
	if err := t.checkFree(force); err != nil {
		return err
	}
	dir := filepath.Dir(t.path)
	if t.defaultDir {
		// ~/.kube is created on write, so probe the nearest directory that exists.
		dir = nearestExistingDir(dir)
	} else if _, err := os.Stat(dir); err != nil {
		return &inputError{msg: fmt.Sprintf("directory %s for the kubeconfig file does not exist or is not readable: %v", dir, err)}
	}
	return probeWritable(dir)
}

func (t kubeconfigTarget) checkFree(force bool) error {
	info, err := os.Lstat(t.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check %s: %w", t.path, err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return &inputError{msg: t.path + " is a symlink; refusing to write a private key through it"}
	case info.IsDir():
		return &inputError{msg: t.path + " is a directory"}
	case !info.Mode().IsRegular():
		return &inputError{msg: t.path + " is not a regular file"}
	case !force:
		return &inputError{msg: existsMessage(t.path)}
	}
	return nil
}

func nearestExistingDir(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// probeWritable catches a read-only mount or a root-owned ~/.kube before a certificate is issued.
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".verda-write-check-*")
	if err != nil {
		return &inputError{msg: fmt.Sprintf("cannot write to %s: %v", dir, err)}
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// Write: without force O_EXCL decides, not Check, so a file that appears after
// Check is never overwritten. Force replaces through a temp file and a rename.
func (t kubeconfigTarget) Write(data []byte, force bool) error {
	if err := t.Check(force); err != nil {
		return err
	}
	if t.defaultDir {
		if err := t.ensureDefaultDir(); err != nil {
			return err
		}
	}
	if !force {
		return t.create(data)
	}
	if _, err := os.Lstat(t.path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return t.create(data)
		}
		return fmt.Errorf("check %s: %w", t.path, err)
	}
	return t.replace(data)
}

// ensureDefaultDir: an existing ~/.kube keeps its mode.
func (t kubeconfigTarget) ensureDefaultDir() error {
	dir := filepath.Dir(t.path)
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := options.MkdirSecure(dir); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

// create needs no chmod: 0600 is already the most permissive result of any umask.
func (t kubeconfigTarget) create(data []byte) error {
	f, err := os.OpenFile(t.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path is the user's --file or the default kubeconfig path
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return errors.New(existsMessage(t.path))
		}
		return fmt.Errorf("create %s: %w", t.path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(t.path)
		return fmt.Errorf("write %s: %w", t.path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(t.path)
		return fmt.Errorf("write %s: %w", t.path, err)
	}
	return nil
}

func (t kubeconfigTarget) replace(data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(t.path), ".verda-kubeconfig-*") // created 0600
	if err != nil {
		return fmt.Errorf("create temp file next to %s: %w", t.path, err)
	}
	tmpPath := tmp.Name()
	cleanup := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(fmt.Errorf("write %s: %w", tmpPath, err))
	}
	// Sync before the rename: a crash must not leave an empty file in place of the old one.
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("write %s: %w", tmpPath, err))
	}
	if err := tmp.Close(); err != nil {
		return cleanup(fmt.Errorf("write %s: %w", tmpPath, err))
	}
	if err := os.Rename(tmpPath, t.path); err != nil {
		return cleanup(fmt.Errorf("replace %s: %w", t.path, err))
	}
	return nil
}
