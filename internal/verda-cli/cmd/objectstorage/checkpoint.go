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

package objectstorage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// checkpointDirName is the subdirectory under the Verda config dir that holds
// one JSON file per in-progress resumable upload.
const checkpointDirName = "s3-uploads"

// checkpointMaxAge bounds how long a stale local checkpoint is kept before GC
// prunes it. The server is authoritative (ListParts reconciles), so this only
// limits local clutter; it is intentionally generous.
const checkpointMaxAge = 7 * 24 * time.Hour

// checkpointPart is one completed part recorded in a checkpoint. ETag is stored
// exactly as the server returned it (quotes included) so Complete can echo it.
type checkpointPart struct {
	N    int32  `json:"n"`
	ETag string `json:"etag"`
}

// checkpoint is the on-disk resume state for a single multipart upload.
// fileSize+mtime form the change-detector; whole-file contents are never hashed.
type checkpoint struct {
	UploadID  string           `json:"uploadId"`
	Bucket    string           `json:"bucket"`
	Key       string           `json:"key"`
	AbsPath   string           `json:"absPath"`
	FileSize  int64            `json:"fileSize"`
	MTime     time.Time        `json:"mtime"`
	PartSize  int64            `json:"partSize"`
	CreatedAt time.Time        `json:"createdAt"`
	Parts     []checkpointPart `json:"parts"`
}

// uploadIdentity is the stable checkpoint key: sha256 over the NUL-separated
// triple (absSourcePath, dstBucket, dstKey). Cheap, deterministic across runs,
// and never depends on file contents.
func uploadIdentity(absPath, bucket, key string) string {
	h := sha256.New()
	h.Write([]byte(absPath))
	h.Write([]byte{0})
	h.Write([]byte(bucket))
	h.Write([]byte{0})
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

// checkpointDir returns ~/.verda/s3-uploads, creating ~/.verda if needed.
func checkpointDir() (string, error) {
	base, err := options.VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, checkpointDirName), nil
}

// checkpointPath maps an identity to its JSON file path.
func checkpointPath(identity string) (string, error) {
	dir, err := checkpointDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, identity+".json"), nil
}

// loadCheckpoint reads the checkpoint for identity. A missing file returns
// (nil, nil) — absence is not an error, it just means "no resume state".
func loadCheckpoint(identity string) (*checkpoint, error) {
	path, err := checkpointPath(identity)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path derived from sha256 identity under ~/.verda
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	var cp checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		// A corrupt checkpoint is treated as absent rather than fatal: the
		// caller falls back to a fresh upload, which is always safe.
		return nil, nil //nolint:nilerr // intentional: unreadable checkpoint → treat as no resume state
	}
	return &cp, nil
}

// saveCheckpoint writes cp atomically (temp file + rename) so a crash mid-write
// never leaves a half-written JSON that would later parse as garbage.
func saveCheckpoint(identity string, cp *checkpoint) error {
	dir, err := checkpointDir()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
		return fmt.Errorf("create checkpoint dir: %w", mkErr)
	}
	path := filepath.Join(dir, identity+".json")
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit checkpoint: %w", err)
	}
	return nil
}

// appendPart records a completed part (updating in place if N already exists)
// and flushes the checkpoint to disk so a crash after this point resumes from
// the new part. Insertion order is not maintained; completeUpload and
// reconcileCheckpoint sort by N before the slice is used.
func appendPart(identity string, cp *checkpoint, n int32, etag string) error {
	for i := range cp.Parts {
		if cp.Parts[i].N == n {
			cp.Parts[i].ETag = etag
			return saveCheckpoint(identity, cp)
		}
	}
	cp.Parts = append(cp.Parts, checkpointPart{N: n, ETag: etag})
	return saveCheckpoint(identity, cp)
}

// deleteCheckpoint removes the checkpoint for identity. A missing file is not
// an error — Complete/abort paths call this unconditionally.
func deleteCheckpoint(identity string) error {
	path, err := checkpointPath(identity)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	return nil
}

// gcCheckpoints prunes stale upload checkpoint and lock files (both live under
// ~/.verda/s3-uploads). A zero maxAge falls back to checkpointMaxAge.
func gcCheckpoints(maxAge time.Duration) error {
	dir, err := checkpointDir()
	if err != nil {
		return err
	}
	return gcStaleFiles(dir, maxAge)
}

// gcStaleFiles removes files in dir whose modtime is older than maxAge (a zero
// maxAge falls back to checkpointMaxAge). Errors on individual files are
// swallowed (best-effort cleanup); a missing directory is a no-op.
func gcStaleFiles(dir string, maxAge time.Duration) error {
	if maxAge <= 0 {
		maxAge = checkpointMaxAge
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read checkpoint dir: %w", err)
	}
	cutoff := time.Now().Add(-maxAge)
	for i := range entries {
		if entries[i].IsDir() {
			continue
		}
		info, infoErr := entries[i].Info()
		if infoErr != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entries[i].Name()))
		}
	}
	return nil
}
