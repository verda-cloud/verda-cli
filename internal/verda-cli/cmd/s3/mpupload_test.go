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

package s3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeMPUploadAPI is a thread-safe, in-memory multipart-upload backend.
//
// It records Create/UploadPart/Complete/Abort/ListParts calls and can be told
// to fail the Nth UploadPart (failAfterPart: succeed for the first N parts,
// then return an error). Parts that "succeed" are remembered so a later
// ListParts reflects server state — letting a resume run see only the missing
// parts. listPartsErr forces ListParts to fail (e.g. NoSuchUpload).
type fakeMPUploadAPI struct {
	API

	mu sync.Mutex

	createCalls    int
	createUploadID string

	uploadedParts map[int32]string // server-side parts: PartNumber -> ETag
	uploadCalls   int
	uploadOrder   []int32 // PartNumbers in the order UploadPart was invoked

	completeCalls int
	completedSet  []s3types.CompletedPart

	abortCalls     int
	abortUploadIDs []string

	listPartsCalls int
	listPartsErr   error
	// partsPageSize: when > 0, ListParts paginates at this many parts per page
	// via PartNumberMarker (mirrors the S3/RGW 1000-part page cap). 0 returns
	// every part in a single page.
	partsPageSize int

	// failAfterPart: when > 0, the (failAfterPart+1)th *successful-so-far*
	// UploadPart and beyond fail. i.e. exactly failAfterPart parts succeed.
	failAfterPart int
	failErr       error
}

func newFakeMPUploadAPI() *fakeMPUploadAPI {
	return &fakeMPUploadAPI{
		createUploadID: "upload-1",
		uploadedParts:  map[int32]string{},
	}
}

func (f *fakeMPUploadAPI) CreateMultipartUpload(ctx context.Context, in *s3.CreateMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CreateMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	return &s3.CreateMultipartUploadOutput{UploadId: aws.String(f.createUploadID)}, nil
}

func (f *fakeMPUploadAPI) UploadPart(ctx context.Context, in *s3.UploadPartInput, opts ...func(*s3.Options)) (*s3.UploadPartOutput, error) {
	// Guard against the checksum regression: a checksum field on UploadPart
	// reintroduces the aws-chunked/CRC32 trailer that RGW rejects.
	if in.ChecksumAlgorithm != "" || in.ChecksumCRC32 != nil || in.ChecksumSHA256 != nil {
		return nil, errors.New("UploadPart must not set any checksum field (RGW compat)")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploadCalls++
	n := aws.ToInt32(in.PartNumber)
	f.uploadOrder = append(f.uploadOrder, n)
	if f.failAfterPart > 0 && len(f.uploadedParts) >= f.failAfterPart {
		err := f.failErr
		if err == nil {
			err = errors.New("injected upload failure")
		}
		return nil, err
	}
	etag := "\"etag-" + string('0'+n) + "\""
	f.uploadedParts[n] = etag
	return &s3.UploadPartOutput{ETag: aws.String(etag)}, nil
}

func (f *fakeMPUploadAPI) ListParts(ctx context.Context, in *s3.ListPartsInput, opts ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listPartsCalls++
	if f.listPartsErr != nil {
		return nil, f.listPartsErr
	}
	parts := make([]s3types.Part, 0, len(f.uploadedParts))
	for n, etag := range f.uploadedParts {
		parts = append(parts, s3types.Part{PartNumber: aws.Int32(n), ETag: aws.String(etag)})
	}
	sort.Slice(parts, func(i, j int) bool {
		return aws.ToInt32(parts[i].PartNumber) < aws.ToInt32(parts[j].PartNumber)
	})

	if f.partsPageSize <= 0 {
		return &s3.ListPartsOutput{Parts: parts, IsTruncated: aws.Bool(false)}, nil
	}

	// Paginate: emit only parts with PartNumber > marker, capped at pageSize.
	marker, _ := strconv.Atoi(aws.ToString(in.PartNumberMarker))
	page := make([]s3types.Part, 0, f.partsPageSize)
	for i := range parts {
		if int(aws.ToInt32(parts[i].PartNumber)) <= marker {
			continue
		}
		page = append(page, parts[i])
		if len(page) == f.partsPageSize {
			break
		}
	}
	truncated := len(page) == f.partsPageSize &&
		aws.ToInt32(page[len(page)-1].PartNumber) < aws.ToInt32(parts[len(parts)-1].PartNumber)
	out := &s3.ListPartsOutput{Parts: page, IsTruncated: aws.Bool(truncated)}
	if truncated {
		out.NextPartNumberMarker = aws.String(strconv.Itoa(int(aws.ToInt32(page[len(page)-1].PartNumber))))
	}
	return out, nil
}

func (f *fakeMPUploadAPI) CompleteMultipartUpload(ctx context.Context, in *s3.CompleteMultipartUploadInput, opts ...func(*s3.Options)) (*s3.CompleteMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completeCalls++
	if in.MultipartUpload != nil {
		f.completedSet = in.MultipartUpload.Parts
	}
	return &s3.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeMPUploadAPI) AbortMultipartUpload(ctx context.Context, in *s3.AbortMultipartUploadInput, opts ...func(*s3.Options)) (*s3.AbortMultipartUploadOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abortCalls++
	f.abortUploadIDs = append(f.abortUploadIDs, aws.ToString(in.UploadId))
	return &s3.AbortMultipartUploadOutput{}, nil
}

// writeTempFile writes size bytes (deterministic pattern) and returns abs path,
// size, and mtime for use as resumableOptions.
func writeTempFile(t *testing.T, size int64) (path string, fsize int64, mtime time.Time) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "big.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return abs, info.Size(), info.ModTime()
}

func optsFor(abs string, size int64, mtime time.Time, partSize int64, concurrency int) *resumableOptions {
	return &resumableOptions{
		AbsPath:     abs,
		Bucket:      "b",
		Key:         "k",
		ContentType: "application/octet-stream",
		FileSize:    size,
		MTime:       mtime,
		PartSize:    partSize,
		Concurrency: concurrency,
	}
}

// verifyCompletedParts asserts the completed set is exactly 1..wantN in
// ascending PartNumber order with non-empty ETags.
func verifyCompletedParts(t *testing.T, parts []s3types.CompletedPart, wantN int) {
	t.Helper()
	if len(parts) != wantN {
		t.Fatalf("completed parts = %d, want %d", len(parts), wantN)
	}
	for i := range parts {
		wantNum := int32(i + 1)
		if aws.ToInt32(parts[i].PartNumber) != wantNum {
			t.Errorf("completed[%d] PartNumber = %d, want %d (must be ascending)", i, aws.ToInt32(parts[i].PartNumber), wantNum)
		}
		if aws.ToString(parts[i].ETag) == "" {
			t.Errorf("completed[%d] ETag empty", i)
		}
	}
}

func TestResumableUpload_Fresh(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 3*minPartSize+100) // 4 parts
	fake := newFakeMPUploadAPI()

	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1)); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.createCalls != 1 {
		t.Errorf("CreateMultipartUpload calls = %d, want 1", fake.createCalls)
	}
	if fake.uploadCalls != 4 {
		t.Errorf("UploadPart calls = %d, want 4", fake.uploadCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	verifyCompletedParts(t, fake.completedSet, 4)

	// Checkpoint deleted on success.
	id := uploadIdentity(abs, "b", "k")
	if cp, _ := loadCheckpoint(id); cp != nil {
		t.Errorf("checkpoint should be deleted after success, got %+v", cp)
	}
}

func TestResumableUpload_BreakAfterPartK_PersistsCheckpoint(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 3*minPartSize+100) // 4 parts
	fake := newFakeMPUploadAPI()
	fake.failAfterPart = 2 // 2 parts succeed, then fail

	err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1))
	if err == nil {
		t.Fatal("expected error when upload breaks mid-way")
	}
	if fake.completeCalls != 0 {
		t.Errorf("Complete must NOT be called on failure, got %d", fake.completeCalls)
	}

	id := uploadIdentity(abs, "b", "k")
	cp, loadErr := loadCheckpoint(id)
	if loadErr != nil {
		t.Fatalf("load checkpoint: %v", loadErr)
	}
	if cp == nil {
		t.Fatal("checkpoint should persist after a mid-upload break")
	}
	if len(cp.Parts) != 2 {
		t.Errorf("persisted parts = %d, want 2", len(cp.Parts))
	}
	if cp.UploadID != "upload-1" {
		t.Errorf("checkpoint UploadID = %q, want upload-1", cp.UploadID)
	}
}

func TestResumableUpload_Resume_OnlyMissingParts(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 3*minPartSize+100) // 4 parts

	// First run: break after 2 parts.
	fake := newFakeMPUploadAPI()
	fake.failAfterPart = 2
	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1)); err == nil {
		t.Fatal("expected first-run failure")
	}

	// Second run: reuse the SAME server (uploadedParts retains parts 1-2),
	// so ListParts reports them and only 3,4 should be uploaded.
	fake.failAfterPart = 0
	fake.uploadOrder = nil
	fake.uploadCalls = 0
	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1)); err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if fake.createCalls != 1 {
		t.Errorf("Create calls = %d, want 1 (resume must not re-create)", fake.createCalls)
	}
	if fake.uploadCalls != 2 {
		t.Errorf("resume UploadPart calls = %d, want 2 (only missing 3,4)", fake.uploadCalls)
	}
	sort.Slice(fake.uploadOrder, func(i, j int) bool { return fake.uploadOrder[i] < fake.uploadOrder[j] })
	if len(fake.uploadOrder) != 2 || fake.uploadOrder[0] != 3 || fake.uploadOrder[1] != 4 {
		t.Errorf("resumed parts = %v, want [3 4]", fake.uploadOrder)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	verifyCompletedParts(t, fake.completedSet, 4)

	id := uploadIdentity(abs, "b", "k")
	if cp, _ := loadCheckpoint(id); cp != nil {
		t.Errorf("checkpoint should be deleted after resumed success, got %+v", cp)
	}
}

func TestResumableUpload_FileChanged_AbortsAndRestarts(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 2*minPartSize+10) // 3 parts
	id := uploadIdentity(abs, "b", "k")

	// Seed a checkpoint from an OLD upload with mismatched size/mtime.
	stale := &checkpoint{
		UploadID:  "stale-upload",
		Bucket:    "b",
		Key:       "k",
		AbsPath:   abs,
		FileSize:  size + 999, // differs -> file changed
		MTime:     mtime.Add(-time.Hour),
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     []checkpointPart{{N: 1, ETag: "\"old\""}},
	}
	if err := saveCheckpoint(id, stale); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	fake := newFakeMPUploadAPI()
	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1)); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.abortCalls != 1 {
		t.Errorf("Abort calls = %d, want 1 (stale upload aborted)", fake.abortCalls)
	}
	if len(fake.abortUploadIDs) != 1 || fake.abortUploadIDs[0] != "stale-upload" {
		t.Errorf("aborted upload IDs = %v, want [stale-upload]", fake.abortUploadIDs)
	}
	if fake.createCalls != 1 {
		t.Errorf("Create calls = %d, want 1 (fresh after abort)", fake.createCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	verifyCompletedParts(t, fake.completedSet, 3)
}

func TestResumableUpload_NoResume_AbortsAndRestarts(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, minPartSize+10) // 2 parts
	id := uploadIdentity(abs, "b", "k")

	// Valid checkpoint (size+mtime match) — only --no-resume forces fresh.
	cp := &checkpoint{
		UploadID:  "prev-upload",
		Bucket:    "b",
		Key:       "k",
		AbsPath:   abs,
		FileSize:  size,
		MTime:     mtime,
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     []checkpointPart{{N: 1, ETag: "\"e1\""}},
	}
	if err := saveCheckpoint(id, cp); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := newFakeMPUploadAPI()
	o := optsFor(abs, size, mtime, minPartSize, 1)
	o.NoResume = true
	if err := resumableUpload(context.Background(), fake, o); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.abortCalls != 1 || fake.abortUploadIDs[0] != "prev-upload" {
		t.Errorf("Abort = %d (%v), want 1 [prev-upload]", fake.abortCalls, fake.abortUploadIDs)
	}
	if fake.listPartsCalls != 0 {
		t.Errorf("ListParts calls = %d, want 0 (--no-resume skips reconcile)", fake.listPartsCalls)
	}
	if fake.createCalls != 1 || fake.completeCalls != 1 {
		t.Errorf("Create/Complete = %d/%d, want 1/1", fake.createCalls, fake.completeCalls)
	}
}

// TestResumableUpload_PartSizeChanged_AbortsAndRestarts guards C-1: when the
// file is unchanged (size+mtime match) but --part-size differs from the stored
// checkpoint, resuming would assemble parts at mismatched boundaries into a
// corrupt object. The run MUST abort the old upload and start fresh instead.
func TestResumableUpload_PartSizeChanged_AbortsAndRestarts(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 2*minPartSize+10) // 3 parts @ minPartSize
	id := uploadIdentity(abs, "b", "k")

	// Valid checkpoint at the OLD part size, with a part already "uploaded".
	stale := &checkpoint{
		UploadID:  "old-upload",
		Bucket:    "b",
		Key:       "k",
		AbsPath:   abs,
		FileSize:  size,
		MTime:     mtime,
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     []checkpointPart{{N: 1, ETag: "\"old1\""}},
	}
	if err := saveCheckpoint(id, stale); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	// Resume with a DIFFERENT part size on the unchanged file.
	fake := newFakeMPUploadAPI()
	o := optsFor(abs, size, mtime, 2*minPartSize, 1)
	if err := resumableUpload(context.Background(), fake, o); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.abortCalls != 1 || fake.abortUploadIDs[0] != "old-upload" {
		t.Errorf("Abort = %d (%v), want 1 [old-upload] (part-size change must abort the old upload)", fake.abortCalls, fake.abortUploadIDs)
	}
	if fake.listPartsCalls != 0 {
		t.Errorf("ListParts calls = %d, want 0 (part-size mismatch must not reconcile/resume)", fake.listPartsCalls)
	}
	if fake.createCalls != 1 {
		t.Errorf("Create calls = %d, want 1 (fresh upload at the new part size)", fake.createCalls)
	}
	// 2*minPartSize over a (2*minPartSize+10)-byte file => exactly 2 parts.
	verifyCompletedParts(t, fake.completedSet, 2)
}

func TestResumableUpload_ListPartsNoSuchUpload_GracefulFresh(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, minPartSize+10) // 2 parts
	id := uploadIdentity(abs, "b", "k")

	cp := &checkpoint{
		UploadID:  "expired-upload",
		Bucket:    "b",
		Key:       "k",
		AbsPath:   abs,
		FileSize:  size,
		MTime:     mtime,
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     []checkpointPart{{N: 1, ETag: "\"e1\""}},
	}
	if err := saveCheckpoint(id, cp); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := newFakeMPUploadAPI()
	fake.listPartsErr = &fakeSmithyError{code: "NoSuchUpload", message: "no such upload"}

	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 1)); err != nil {
		t.Fatalf("resumableUpload should gracefully restart on NoSuchUpload, got: %v", err)
	}
	if fake.listPartsCalls != 1 {
		t.Errorf("ListParts calls = %d, want 1", fake.listPartsCalls)
	}
	if fake.createCalls != 1 {
		t.Errorf("Create calls = %d, want 1 (fresh after NoSuchUpload)", fake.createCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	verifyCompletedParts(t, fake.completedSet, 2)
}

func TestResumableUpload_Concurrency_OrderedComplete(t *testing.T) {
	withTempVerdaHome(t)
	abs, size, mtime := writeTempFile(t, 7*minPartSize+5) // 8 parts
	fake := newFakeMPUploadAPI()

	if err := resumableUpload(context.Background(), fake, optsFor(abs, size, mtime, minPartSize, 4)); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.uploadCalls != 8 {
		t.Errorf("UploadPart calls = %d, want 8", fake.uploadCalls)
	}
	// Despite concurrent uploads (possibly out of order), Complete's part set
	// must be ascending and contain all 8.
	verifyCompletedParts(t, fake.completedSet, 8)
}

// TestResumableUpload_Resume_BeyondFirstPage seeds a server with more than one
// ListParts page worth of parts (>1000) and verifies the resumed run uploads
// ZERO parts. A non-paginated ListParts would only see parts 1..1000, mark
// 1001+ as missing, and re-upload them — wasting the entire resume value on the
// feature's primary multi-GB workload. The file is never read on disk because
// every part is already on the server, so a fabricated FileSize is safe here.
func TestResumableUpload_Resume_BeyondFirstPage(t *testing.T) {
	withTempVerdaHome(t)

	const totalParts = 1001
	abs, _, mtime := writeTempFile(t, 1) // tiny: never opened (no missing parts)
	fileSize := int64(totalParts) * minPartSize
	id := uploadIdentity(abs, "b", "k")

	// Server already holds every part across multiple pages.
	fake := newFakeMPUploadAPI()
	fake.partsPageSize = 1000
	parts := make([]checkpointPart, 0, totalParts)
	for n := int32(1); n <= totalParts; n++ {
		etag := "\"etag-" + strconv.Itoa(int(n)) + "\""
		fake.uploadedParts[n] = etag
		parts = append(parts, checkpointPart{N: n, ETag: etag})
	}

	// Local checkpoint matches size+mtime so resume (not fresh) is chosen.
	cp := &checkpoint{
		UploadID:  fake.createUploadID,
		Bucket:    "b",
		Key:       "k",
		AbsPath:   abs,
		FileSize:  fileSize,
		MTime:     mtime,
		PartSize:  minPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     parts,
	}
	if err := saveCheckpoint(id, cp); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}

	o := optsFor(abs, fileSize, mtime, minPartSize, 1)
	if err := resumableUpload(context.Background(), fake, o); err != nil {
		t.Fatalf("resumableUpload: %v", err)
	}
	if fake.uploadCalls != 0 {
		t.Errorf("resume UploadPart calls = %d, want 0 (all %d parts already on server)", fake.uploadCalls, totalParts)
	}
	if fake.createCalls != 0 {
		t.Errorf("Create calls = %d, want 0 (resume must not re-create)", fake.createCalls)
	}
	if fake.listPartsCalls < 2 {
		t.Errorf("ListParts calls = %d, want >= 2 (must paginate past the 1000-part page)", fake.listPartsCalls)
	}
	if fake.completeCalls != 1 {
		t.Errorf("Complete calls = %d, want 1", fake.completeCalls)
	}
	verifyCompletedParts(t, fake.completedSet, totalParts)

	if leftover, _ := loadCheckpoint(id); leftover != nil {
		t.Errorf("checkpoint should be deleted after resumed success, got %+v", leftover)
	}
}

func TestComputePartSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		fileSize  int64
		requested int64
		wantMin   int64
		wantExact int64 // 0 = don't assert an exact size
	}{
		{"below floor bumps to 5MiB", 100 * 1024 * 1024, 1024, minPartSize, 0},
		{"zero requests auto", 100 * 1024 * 1024, 0, minPartSize, 0},
		{"just over maxParts*floor scales up", maxParts*minPartSize + 1, minPartSize, minPartSize * 2, minPartSize * 2},
		// Exact multiple of maxParts*floor needs exactly maxParts parts, which
		// is allowed — it must NOT be bumped (regression for the off-by-one).
		{"exact maxParts multiple stays at floor", maxParts * minPartSize, minPartSize, minPartSize, minPartSize},
	}
	for _, tc := range cases {
		got := computePartSize(tc.fileSize, tc.requested)
		if got < minPartSize {
			t.Errorf("%s: part size %d below floor", tc.name, got)
		}
		// Correct ceil check: the file must split into at most maxParts parts.
		if (tc.fileSize+got-1)/got > maxParts {
			t.Errorf("%s: part size %d yields > maxParts parts", tc.name, got)
		}
		if got < tc.wantMin {
			t.Errorf("%s: part size = %d, want >= %d", tc.name, got, tc.wantMin)
		}
		if tc.wantExact != 0 && got != tc.wantExact {
			t.Errorf("%s: part size = %d, want exactly %d", tc.name, got, tc.wantExact)
		}
	}
}

func TestPartRange(t *testing.T) {
	t.Parallel()
	const ps = minPartSize
	fileSize := 2*ps + 123
	wantRanges := [][2]int64{
		{0, ps},
		{ps, 2 * ps},
		{2 * ps, fileSize},
	}
	for i := range wantRanges {
		n := int32(i + 1)
		start, end := partRange(n, fileSize, ps)
		if start != wantRanges[i][0] || end != wantRanges[i][1] {
			t.Errorf("part %d range = [%d,%d), want [%d,%d)", n, start, end, wantRanges[i][0], wantRanges[i][1])
		}
	}
}
