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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// defaultDownloadPartSize is the chunk size for resumable downloads. Unlike
// uploads there is no 5 MiB floor or 10000-chunk ceiling on the download side.
const defaultDownloadPartSize int64 = 8 * 1024 * 1024

const downloadCheckpointDirName = "s3-downloads"

// downloadCheckpoint is the on-disk resume state for a single download. ETag +
// TotalSize are the change-detector: if the remote object changes, they won't
// match and the download restarts. Chunks holds the completed 1-indexed chunks.
type downloadCheckpoint struct {
	Bucket    string    `json:"bucket"`
	Key       string    `json:"key"`
	ETag      string    `json:"etag"`
	DestPath  string    `json:"destPath"`
	TotalSize int64     `json:"totalSize"`
	PartSize  int64     `json:"partSize"`
	CreatedAt time.Time `json:"createdAt"`
	Chunks    []int32   `json:"chunks"`
}

// resumableDownloadOptions parameterizes a single resumable download.
type resumableDownloadOptions struct {
	Bucket      string
	Key         string
	DestPath    string
	PartSize    int64 // 0 -> default
	Concurrency int   // 0 -> default
	NoResume    bool
	// OnProgress, if set, is called with (doneBytes, totalBytes) after the
	// initial reconcile and after each chunk. Calls are serialized.
	OnProgress func(done, total int64)
	// OnResume, if set, is called once with (alreadyBytes, totalBytes) when the
	// download is continuing from a checkpoint (some chunks already on disk).
	OnResume func(already, total int64)
}

func downloadIdentity(absDest, bucket, key string) string {
	h := sha256.New()
	h.Write([]byte(absDest))
	h.Write([]byte{0})
	h.Write([]byte(bucket))
	h.Write([]byte{0})
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}

func downloadCheckpointPath(identity string) (string, error) {
	base, err := options.VerdaDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, downloadCheckpointDirName, identity+".json"), nil
}

func loadDownloadCheckpoint(identity string) *downloadCheckpoint {
	path, err := downloadCheckpointPath(identity)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path derived from sha256 identity under ~/.verda
	if err != nil {
		return nil
	}
	var cp downloadCheckpoint
	if json.Unmarshal(data, &cp) != nil {
		return nil // corrupt -> treat as absent
	}
	return &cp
}

func saveDownloadCheckpoint(identity string, cp *downloadCheckpoint) error {
	path, err := downloadCheckpointPath(identity)
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return fmt.Errorf("create download checkpoint dir: %w", mkErr)
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("marshal download checkpoint: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write download checkpoint: %w", err)
	}
	return os.Rename(tmp, path)
}

func deleteDownloadCheckpoint(identity string) {
	if path, err := downloadCheckpointPath(identity); err == nil {
		_ = os.Remove(path)
	}
}

// numChunks is the chunk count for total bytes at partSize (ceil division).
func numChunks(total, partSize int64) int32 {
	if total <= 0 {
		return 0
	}
	n := total / partSize
	if total%partSize != 0 {
		n++
	}
	return int32(n)
}

// chunkRange returns the [start,end) byte range for chunk n (1-indexed).
func chunkRange(n int32, total, partSize int64) (start, end int64) {
	start = int64(n-1) * partSize
	end = min(start+partSize, total)
	return start, end
}

type downloadChunkResult struct {
	n   int32
	err error
}

// resumableDownload downloads opts.Bucket/opts.Key to opts.DestPath using
// concurrent ranged GETs, resuming from a local checkpoint + ".part" file when
// the remote object is unchanged (ETag + size). It writes to "<dest>.part" and
// renames on success. Returns the object's total size. Only the API interface
// is used, so it is fully fakeable.
func resumableDownload(ctx context.Context, client API, opts *resumableDownloadOptions) (int64, error) {
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(opts.Bucket),
		Key:    aws.String(opts.Key),
	})
	if err != nil {
		return 0, translateError(err)
	}
	total := aws.ToInt64(head.ContentLength)
	etag := aws.ToString(head.ETag)

	partSize := opts.PartSize
	if partSize <= 0 {
		partSize = defaultDownloadPartSize
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = defaultConcurrency
	}

	absDest, err := filepath.Abs(opts.DestPath)
	if err != nil {
		return 0, err
	}
	identity := downloadIdentity(absDest, opts.Bucket, opts.Key)

	// Same-host guard: two downloads of the same object would race the .part file.
	release, acquired, err := acquireTransferLock(identity)
	if err != nil {
		return 0, err
	}
	if !acquired {
		return 0, fmt.Errorf("a download of s3://%s/%s is already in progress on this machine", opts.Bucket, opts.Key)
	}
	defer release()

	partPath := opts.DestPath + ".part"
	cp := loadDownloadCheckpoint(identity)
	done := map[int32]bool{}
	if !opts.NoResume && cp != nil && cp.ETag == etag && cp.TotalSize == total && cp.PartSize == partSize && fileExists(partPath) {
		for _, n := range cp.Chunks {
			done[n] = true
		}
	} else {
		_ = os.Remove(partPath) // stale/changed -> start over
		cp = &downloadCheckpoint{
			Bucket: opts.Bucket, Key: opts.Key, ETag: etag, DestPath: opts.DestPath,
			TotalSize: total, PartSize: partSize, CreatedAt: time.Now().UTC(),
		}
		if err := saveDownloadCheckpoint(identity, cp); err != nil {
			return 0, err
		}
	}

	if len(done) > 0 && opts.OnResume != nil {
		opts.OnResume(min(int64(len(done))*partSize, total), total)
	}

	if err := os.MkdirAll(filepath.Dir(opts.DestPath), 0o750); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- caller-specified destination
	if err != nil {
		return 0, err
	}

	if err := downloadMissingChunks(ctx, client, opts, identity, cp, file, etag, total, partSize, concurrency, done); err != nil {
		_ = file.Close()
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(partPath, opts.DestPath); err != nil {
		return 0, fmt.Errorf("finalize download: %w", err)
	}
	deleteDownloadCheckpoint(identity)
	return total, nil
}

// downloadMissingChunks fetches every chunk not already in done with a bounded
// worker pool, writing each at its offset and recording it in the checkpoint.
func downloadMissingChunks(ctx context.Context, client API, opts *resumableDownloadOptions, identity string, cp *downloadCheckpoint, file *os.File, etag string, total, partSize int64, concurrency int, done map[int32]bool) error {
	totalChunks := numChunks(total, partSize)
	if opts.OnProgress != nil {
		opts.OnProgress(min(int64(len(done))*partSize, total), total)
	}

	missing := make([]int32, 0, int(totalChunks))
	for n := int32(1); n <= totalChunks; n++ {
		if !done[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int32)
	results := make(chan downloadChunkResult)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				err := downloadOneChunk(ctx, client, opts.Bucket, opts.Key, etag, file, n, total, partSize)
				select {
				case results <- downloadChunkResult{n: n, err: err}:
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

	completed := int64(len(done))
	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
				cancel()
			}
			continue
		}
		cp.Chunks = append(cp.Chunks, res.n)
		if err := saveDownloadCheckpoint(identity, cp); err != nil && firstErr == nil {
			firstErr = err
			cancel()
			continue
		}
		completed++
		if opts.OnProgress != nil {
			opts.OnProgress(min(completed*partSize, total), total)
		}
	}
	return firstErr
}

// downloadOneChunk fetches chunk n via a ranged, If-Match GET and writes it at
// the chunk's offset. If-Match makes the server reject (412) a changed object.
func downloadOneChunk(ctx context.Context, client API, bucket, key, etag string, file *os.File, n int32, total, partSize int64) error {
	start, end := chunkRange(n, total, partSize)
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(key),
		Range:   aws.String(fmt.Sprintf("bytes=%d-%d", start, end-1)),
		IfMatch: aws.String(etag),
	})
	if err != nil {
		return translateError(err)
	}
	defer func() { _ = out.Body.Close() }()
	if _, err := io.Copy(io.NewOffsetWriter(file, start), out.Body); err != nil {
		return fmt.Errorf("write chunk %d: %w", n, err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
