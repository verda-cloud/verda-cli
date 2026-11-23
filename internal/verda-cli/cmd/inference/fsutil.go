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

package inference

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const backupSuffix = ".verda-bak"

// readFileIfExists returns nil data and exists=false for a missing file.
func readFileIfExists(path string) (data []byte, exists bool, err error) {
	data, err = os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// resolveWritePath follows symlinks so dotfile-manager links keep pointing at
// the real file instead of being replaced by a regular file on rename.
func resolveWritePath(path string) string {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target
	}
	return path
}

// writeFileAtomic writes via a temp file in the same directory and renames it
// over path. An existing file keeps its mode unless forceMode is set.
func writeFileAtomic(path string, data []byte, mode fs.FileMode, forceMode bool) error {
	path = resolveWritePath(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", dir, err)
	}
	if st, err := os.Stat(path); err == nil && !forceMode {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".verda-tmp-*")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("cannot set permissions on %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// backupOnce copies path to path.verda-bak unless a backup already exists, so
// the backup always holds the content from before verda first touched it.
func backupOnce(path string, data []byte) (string, error) {
	backup := resolveWritePath(path) + backupSuffix
	if _, err := os.Stat(backup); err == nil {
		return backup, nil
	}
	mode := fs.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := writeFileAtomic(backup, data, mode, true); err != nil {
		return "", err
	}
	return backup, nil
}

func removeFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
