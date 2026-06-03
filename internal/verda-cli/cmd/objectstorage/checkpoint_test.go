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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempVerdaHome points VERDA_HOME at a temp dir so checkpoint I/O never
// touches the developer's real ~/.verda. Returns a restore func.
func withTempVerdaHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)
}

func TestUploadIdentity_StableAndDistinct(t *testing.T) {
	t.Parallel()
	a := uploadIdentity("/abs/big.bin", "bucket", "key")
	b := uploadIdentity("/abs/big.bin", "bucket", "key")
	if a != b {
		t.Fatalf("identity not stable: %q vs %q", a, b)
	}
	// NUL separation: changing the boundary between fields must change identity.
	c := uploadIdentity("/abs/big.binbucket", "", "key")
	if a == c {
		t.Fatalf("identity collision across field boundary: %q", a)
	}
	for _, other := range []string{
		uploadIdentity("/abs/other.bin", "bucket", "key"),
		uploadIdentity("/abs/big.bin", "other", "key"),
		uploadIdentity("/abs/big.bin", "bucket", "other"),
	} {
		if a == other {
			t.Fatalf("identity should differ: %q", a)
		}
	}
}

func TestCheckpoint_SaveLoadDelete(t *testing.T) {
	withTempVerdaHome(t)
	id := uploadIdentity("/abs/f", "b", "k")

	if got, err := loadCheckpoint(id); err != nil || got != nil {
		t.Fatalf("load before save = (%v, %v), want (nil, nil)", got, err)
	}

	cp := &checkpoint{
		UploadID:  "u1",
		Bucket:    "b",
		Key:       "k",
		AbsPath:   "/abs/f",
		FileSize:  100,
		MTime:     time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
	}
	if err := saveCheckpoint(id, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := loadCheckpoint(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil || got.UploadID != "u1" || got.FileSize != 100 {
		t.Fatalf("loaded = %+v, want UploadID=u1 FileSize=100", got)
	}
	if !got.MTime.Equal(cp.MTime) {
		t.Errorf("mtime = %v, want %v", got.MTime, cp.MTime)
	}

	if err := deleteCheckpoint(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, err := loadCheckpoint(id); err != nil || got != nil {
		t.Fatalf("load after delete = (%v, %v), want (nil, nil)", got, err)
	}
	// Delete is idempotent.
	if err := deleteCheckpoint(id); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestCheckpoint_AppendPart(t *testing.T) {
	withTempVerdaHome(t)
	id := uploadIdentity("/abs/f", "b", "k")
	cp := &checkpoint{UploadID: "u1", Bucket: "b", Key: "k", AbsPath: "/abs/f"}
	if err := saveCheckpoint(id, cp); err != nil {
		t.Fatalf("save: %v", err)
	}

	for n := int32(1); n <= 3; n++ {
		if err := appendPart(id, cp, n, "etag"+string('0'+n)); err != nil {
			t.Fatalf("append %d: %v", n, err)
		}
	}
	// Re-appending the same part updates rather than duplicates.
	if err := appendPart(id, cp, 2, "etag2-new"); err != nil {
		t.Fatalf("re-append: %v", err)
	}

	got, err := loadCheckpoint(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Parts) != 3 {
		t.Fatalf("parts = %d, want 3 (no dup)", len(got.Parts))
	}
	for i := range got.Parts {
		if got.Parts[i].N == 2 && got.Parts[i].ETag != "etag2-new" {
			t.Errorf("part 2 etag = %q, want etag2-new", got.Parts[i].ETag)
		}
	}
}

func TestCheckpoint_LoadCorruptIsAbsent(t *testing.T) {
	withTempVerdaHome(t)
	id := uploadIdentity("/abs/f", "b", "k")
	path, err := checkpointPath(id)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadCheckpoint(id)
	if err != nil {
		t.Fatalf("load corrupt returned err: %v", err)
	}
	if got != nil {
		t.Fatalf("corrupt checkpoint should load as nil, got %+v", got)
	}
}

func TestGCCheckpoints_PrunesOld(t *testing.T) {
	withTempVerdaHome(t)
	dir, err := checkpointDir()
	if err != nil {
		t.Fatalf("dir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldFile := filepath.Join(dir, "old.json")
	newFile := filepath.Join(dir, "new.json")
	for _, p := range []string{oldFile, newFile} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(oldFile, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := gcCheckpoints(0); err != nil {
		t.Fatalf("gc: %v", err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old checkpoint should be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Errorf("new checkpoint should survive, stat err = %v", err)
	}
}

func TestGCCheckpoints_MissingDirNoError(t *testing.T) {
	withTempVerdaHome(t)
	if err := gcCheckpoints(0); err != nil {
		t.Fatalf("gc on missing dir = %v, want nil", err)
	}
}
