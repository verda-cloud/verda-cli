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

//go:build !windows

package objectstorage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireTransferLock takes a non-blocking advisory exclusive lock keyed by the
// upload identity, so two processes can't push the same object concurrently
// (which would race on the checkpoint and double-upload parts). Returns
// acquired=false when another process already holds it. The lock is released by
// the returned func, and the OS frees it automatically when the process exits —
// so a crash leaves no stale lock.
func acquireTransferLock(identity string) (release func(), acquired bool, err error) {
	dir, err := checkpointDir()
	if err != nil {
		return nil, false, err
	}
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return nil, false, fmt.Errorf("create lock dir: %w", mkErr)
	}
	path := filepath.Join(dir, identity+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path derived from sha256 identity under ~/.verda
	if err != nil {
		return nil, false, fmt.Errorf("open lock file: %w", err)
	}
	if flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil { //nolint:gosec // G115: a file descriptor always fits in int
		_ = f.Close()
		if errors.Is(flockErr, syscall.EWOULDBLOCK) {
			return nil, false, nil // held by another process
		}
		return nil, false, fmt.Errorf("lock %q: %w", path, flockErr)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // G115: a file descriptor always fits in int
		_ = f.Close()
	}, true, nil
}
