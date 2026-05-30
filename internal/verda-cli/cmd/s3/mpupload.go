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
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	// minPartSize is the S3/RGW floor for every part except the last (5 MiB).
	minPartSize int64 = 5 * 1024 * 1024
	// maxParts is the hard S3 ceiling on parts per multipart upload.
	maxParts int64 = 10000
	// defaultConcurrency is the worker-pool width when the caller passes 0.
	defaultConcurrency = 5
)

// resumableOptions parameterizes a single resumable multipart upload.
// PartSize/Concurrency of 0 fall back to computed/default values.
type resumableOptions struct {
	AbsPath     string
	Bucket      string
	Key         string
	ContentType string
	FileSize    int64
	MTime       time.Time
	PartSize    int64
	Concurrency int
	NoResume    bool
	// OnProgress, if set, is called with (completedParts, totalParts) after the
	// initial server reconcile and after each part finishes. Calls are
	// serialized (safe to drive a progress bar). nil disables reporting.
	OnProgress func(done, total int32)
}

// computePartSize returns a part size >= minPartSize, scaled up so the file
// splits into at most maxParts parts. A requested size below the floor or that
// would exceed maxParts is bumped to the smallest valid value.
func computePartSize(fileSize, requested int64) int64 {
	size := requested
	if size < minPartSize {
		size = minPartSize
	}
	// Double until the file splits into at most maxParts parts. Ceil division:
	// a file that is an exact multiple of size*maxParts needs exactly maxParts
	// parts (allowed) and must NOT be bumped — the old `fileSize/size+1` form
	// fired on that boundary and needlessly doubled the part size.
	for (fileSize+size-1)/size > maxParts {
		size *= 2
	}
	return size
}

// numParts returns the part count for fileSize at partSize (ceil division).
// A zero-length file is still one part for the multipart machinery, but the
// resumable path is never entered for files <= partSize (see cp routing).
func numParts(fileSize, partSize int64) int32 {
	if fileSize == 0 {
		return 1
	}
	n := fileSize / partSize
	if fileSize%partSize != 0 {
		n++
	}
	// computePartSize guarantees n <= maxParts (10000), well within int32.
	if n > maxParts {
		n = maxParts
	}
	return int32(n)
}

// partRange returns the deterministic byte range [start,end) for part n
// (1-indexed) of a fileSize-byte file at partSize. The last part is short.
func partRange(n int32, fileSize, partSize int64) (start, end int64) {
	start = int64(n-1) * partSize
	end = start + partSize
	if end > fileSize {
		end = fileSize
	}
	return start, end
}

// uploadPartResult carries one worker's outcome back to the collector.
type uploadPartResult struct {
	n    int32
	etag string
	err  error
}

// resumableUpload runs (or resumes) a multipart upload of opts.AbsPath to
// opts.Bucket/opts.Key using only the API interface, so it is fully fakeable.
//
// Decision tree (design §2): a valid local checkpoint whose size+mtime match
// and whose UploadId the server still recognizes (ListParts) resumes, uploading
// only the parts the server lacks; otherwise it starts fresh, proactively
// aborting any stale upload it was about to abandon. The server is always
// authoritative — the local checkpoint is a hint reconciled against ListParts.
func resumableUpload(ctx context.Context, client API, opts *resumableOptions) error {
	partSize := computePartSize(opts.FileSize, opts.PartSize)
	identity := uploadIdentity(opts.AbsPath, opts.Bucket, opts.Key)

	// Same-host guard: refuse a second concurrent upload of this object so two
	// processes can't race on the checkpoint and double-upload parts.
	release, acquired, err := acquireUploadLock(identity)
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("an upload of s3://%s/%s is already in progress on this machine", opts.Bucket, opts.Key)
	}
	defer release()

	cp, uploadID, err := resolveUpload(ctx, client, opts, identity, partSize)
	if err != nil {
		return err
	}

	done := make(map[int32]string, len(cp.Parts))
	for i := range cp.Parts {
		done[cp.Parts[i].N] = cp.Parts[i].ETag
	}

	total := numParts(opts.FileSize, partSize)
	if opts.OnProgress != nil {
		opts.OnProgress(int32(len(done)), total) // reflect parts already on the server
	}
	if err := uploadMissingParts(ctx, client, opts, identity, cp, partSize, total, done); err != nil {
		return err
	}

	if err := completeUpload(ctx, client, opts, uploadID, cp); err != nil {
		return err
	}
	return deleteCheckpoint(identity)
}

// resolveUpload walks the decision tree and returns a checkpoint + UploadId
// that is ready to (re)use. On any fresh path it has already created a new
// multipart upload and persisted the initial checkpoint; on resume it returns
// the reconciled checkpoint backed by ListParts.
func resolveUpload(ctx context.Context, client API, opts *resumableOptions, identity string, partSize int64) (*checkpoint, string, error) {
	existing, err := loadCheckpoint(identity)
	if err != nil {
		return nil, "", err
	}

	fresh := func() (*checkpoint, string, error) {
		if existing != nil && existing.UploadID != "" {
			// Self-cleanup: never strand our own prior upload's parts.
			_, _ = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(opts.Bucket),
				Key:      aws.String(opts.Key),
				UploadId: aws.String(existing.UploadID),
			})
		}
		return startFresh(ctx, client, opts, identity, partSize)
	}

	if existing == nil || opts.NoResume {
		return fresh()
	}
	if existing.FileSize != opts.FileSize || !existing.MTime.Equal(opts.MTime) {
		return fresh()
	}

	listed, err := listAllParts(ctx, client, opts.Bucket, opts.Key, existing.UploadID)
	if err != nil {
		if isNoSuchUpload(err) {
			_ = deleteCheckpoint(identity)
			return startFresh(ctx, client, opts, identity, partSize)
		}
		return nil, "", translateError(err)
	}

	reconciled := reconcileCheckpoint(existing, listed)
	if err := saveCheckpoint(identity, reconciled); err != nil {
		return nil, "", err
	}
	return reconciled, reconciled.UploadID, nil
}

// startFresh creates a new multipart upload and writes the initial checkpoint
// (no parts yet). Returns the checkpoint and its UploadId.
func startFresh(ctx context.Context, client API, opts *resumableOptions, identity string, partSize int64) (*checkpoint, string, error) {
	out, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(opts.Bucket),
		Key:         aws.String(opts.Key),
		ContentType: aws.String(opts.ContentType),
	})
	if err != nil {
		return nil, "", translateError(err)
	}
	uploadID := aws.ToString(out.UploadId)
	cp := &checkpoint{
		UploadID:  uploadID,
		Bucket:    opts.Bucket,
		Key:       opts.Key,
		AbsPath:   opts.AbsPath,
		FileSize:  opts.FileSize,
		MTime:     opts.MTime,
		PartSize:  partSize,
		CreatedAt: time.Now().UTC(),
	}
	if err := saveCheckpoint(identity, cp); err != nil {
		// Don't strand the just-created server-side upload (it consumes storage
		// and would be invisible to `ls`) if we can't persist its checkpoint.
		_, _ = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(opts.Bucket),
			Key:      aws.String(opts.Key),
			UploadId: aws.String(uploadID),
		})
		return nil, "", err
	}
	return cp, uploadID, nil
}

// listAllParts paginates ListParts via PartNumberMarker and returns every
// part the server holds for uploadID. S3/RGW caps each page at 1000 parts, so
// a single call would silently drop parts 1001+ — and resume would re-upload
// everything past the first page. Returns the raw API error (untranslated) so
// the caller can detect NoSuchUpload before mapping it.
func listAllParts(ctx context.Context, client API, bucket, key, uploadID string) ([]s3types.Part, error) {
	var (
		parts  []s3types.Part
		marker *string
	)
	for {
		in := &s3.ListPartsInput{
			Bucket:   aws.String(bucket),
			Key:      aws.String(key),
			UploadId: aws.String(uploadID),
		}
		if marker != nil {
			in.PartNumberMarker = marker
		}
		out, err := client.ListParts(ctx, in)
		if err != nil {
			return nil, err
		}
		parts = append(parts, out.Parts...)
		if !aws.ToBool(out.IsTruncated) || aws.ToString(out.NextPartNumberMarker) == "" {
			return parts, nil
		}
		marker = out.NextPartNumberMarker
	}
}

// reconcileCheckpoint rebuilds the parts list from the server's ListParts
// output — the server is authoritative. The local checkpoint's UploadId and
// metadata are preserved; only the completed-parts set is replaced.
func reconcileCheckpoint(cp *checkpoint, listed []s3types.Part) *checkpoint {
	parts := make([]checkpointPart, 0, len(listed))
	for i := range listed {
		parts = append(parts, checkpointPart{
			N:    aws.ToInt32(listed[i].PartNumber),
			ETag: aws.ToString(listed[i].ETag),
		})
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].N < parts[j].N })
	out := *cp
	out.Parts = parts
	return &out
}

// uploadMissingParts uploads every part in [1,total] not already in done,
// using a bounded worker pool. Each successful part is appended to the
// checkpoint and flushed before the next is acknowledged, so a crash resumes
// from the last persisted part. Checkpoint mutation is serialized by mu.
func uploadMissingParts(ctx context.Context, client API, opts *resumableOptions, identity string, cp *checkpoint, partSize int64, total int32, done map[int32]string) error {
	missing := make([]int32, 0, int(total))
	for n := int32(1); n <= total; n++ {
		if _, ok := done[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int32)
	results := make(chan uploadPartResult)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				etag, err := uploadOnePart(ctx, client, opts, cp.UploadID, n, partSize)
				select {
				case results <- uploadPartResult{n: n, etag: etag, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, n := range missing {
			select {
			case jobs <- n:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var mu sync.Mutex
	var firstErr error
	completed := int32(len(done)) // parts already on the server count toward progress
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
				cancel()
			}
			continue
		}
		mu.Lock()
		err := appendPart(identity, cp, res.n, res.etag)
		mu.Unlock()
		if err != nil && firstErr == nil {
			firstErr = err
			cancel()
			continue
		}
		completed++
		if opts.OnProgress != nil {
			opts.OnProgress(completed, total)
		}
	}
	return firstErr
}

// uploadOnePart reads part n's deterministic byte range from the local file and
// uploads it. CRITICAL: no ChecksumAlgorithm/checksum fields are set — that
// would reintroduce aws-chunked/CRC32 trailers and break RGW (400
// XAmzContentSHA256Mismatch). ContentLength is set so the non-seekable section
// reader does not trigger chunked transfer-encoding.
func uploadOnePart(ctx context.Context, client API, opts *resumableOptions, uploadID string, n int32, partSize int64) (string, error) {
	f, err := os.Open(opts.AbsPath) // #nosec G304 -- AbsPath is the user-specified upload source
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = f.Close() }()

	start, end := partRange(n, opts.FileSize, partSize)
	section := io.NewSectionReader(f, start, end-start)

	out, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(opts.Bucket),
		Key:           aws.String(opts.Key),
		UploadId:      aws.String(uploadID),
		PartNumber:    aws.Int32(n),
		Body:          section,
		ContentLength: aws.Int64(end - start),
	})
	if err != nil {
		return "", translateError(err)
	}
	return aws.ToString(out.ETag), nil
}

// completeUpload finalizes the multipart upload with the full ordered part set
// from the checkpoint. Parts MUST be ascending by PartNumber.
func completeUpload(ctx context.Context, client API, opts *resumableOptions, uploadID string, cp *checkpoint) error {
	if len(cp.Parts) == 0 {
		return errNoParts
	}
	sort.Slice(cp.Parts, func(i, j int) bool { return cp.Parts[i].N < cp.Parts[j].N })
	parts := make([]s3types.CompletedPart, 0, len(cp.Parts))
	for i := range cp.Parts {
		parts = append(parts, s3types.CompletedPart{
			ETag:       aws.String(cp.Parts[i].ETag),
			PartNumber: aws.Int32(cp.Parts[i].N),
		})
	}
	_, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(opts.Bucket),
		Key:             aws.String(opts.Key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return translateError(err)
	}
	return nil
}

// errNoParts guards Complete against an empty part set (defensive; the upload
// loop always produces at least one part for a non-empty file).
var errNoParts = errors.New("multipart upload has no parts to complete")
