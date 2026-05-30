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

package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// dlFakeAPI serves a fixed object via Head + ranged Get, recording calls and
// optionally failing a specific chunk (to simulate a mid-download break).
type dlFakeAPI struct {
	API
	content   []byte
	etag      string
	partSize  int64
	failChunk int32

	mu       sync.Mutex
	getCalls int
}

func (d *dlFakeAPI) HeadObject(ctx context.Context, in *s3.HeadObjectInput, opts ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(d.content))), ETag: aws.String(d.etag)}, nil
}

func (d *dlFakeAPI) GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	var start, end int64
	if _, err := fmt.Sscanf(aws.ToString(in.Range), "bytes=%d-%d", &start, &end); err != nil {
		return nil, fmt.Errorf("bad range %q", aws.ToString(in.Range))
	}
	n := int32(start/d.partSize) + 1
	d.mu.Lock()
	d.getCalls++
	d.mu.Unlock()
	if d.failChunk != 0 && n == d.failChunk {
		return nil, errors.New("injected get failure")
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(d.content[start : end+1]))}, nil
}

func (d *dlFakeAPI) calls() int { d.mu.Lock(); defer d.mu.Unlock(); return d.getCalls }

func TestResumableDownload_Fresh(t *testing.T) {
	withTempVerdaHome(t)
	t.Chdir(t.TempDir())
	content := bytes.Repeat([]byte("ab"), 1280) // 2560 bytes -> 3 chunks at 1 KiB
	fake := &dlFakeAPI{content: content, etag: "\"e\"", partSize: 1024}

	resumeCalled := false
	n, err := resumableDownload(context.Background(), fake, &resumableDownloadOptions{
		Bucket: "b", Key: "k", DestPath: "out.bin", PartSize: 1024, Concurrency: 1,
		OnResume: func(already, total int64) { resumeCalled = true },
	})
	if err != nil {
		t.Fatalf("resumableDownload: %v", err)
	}
	if resumeCalled {
		t.Error("OnResume should not fire on a fresh download")
	}
	if n != int64(len(content)) {
		t.Errorf("size = %d, want %d", n, len(content))
	}
	got, rerr := os.ReadFile("out.bin")
	if rerr != nil || !bytes.Equal(got, content) {
		t.Errorf("downloaded file mismatch (err=%v)", rerr)
	}
	if fake.calls() != 3 {
		t.Errorf("GetObject calls = %d, want 3", fake.calls())
	}
	if _, statErr := os.Stat("out.bin.part"); statErr == nil {
		t.Error(".part file should be renamed away on success")
	}
}

func TestResumableDownload_BreakThenResume(t *testing.T) {
	withTempVerdaHome(t)
	t.Chdir(t.TempDir())
	content := bytes.Repeat([]byte("xy"), 1280) // 2560 bytes -> 3 chunks
	dst := "model.bin"

	// Run 1: chunk 2 fails after chunk 1 succeeds (concurrency 1 -> ordered).
	fake := &dlFakeAPI{content: content, etag: "\"e\"", partSize: 1024, failChunk: 2}
	if _, err := resumableDownload(context.Background(), fake, &resumableDownloadOptions{
		Bucket: "b", Key: "k", DestPath: dst, PartSize: 1024, Concurrency: 1,
	}); err == nil {
		t.Fatal("expected first run to fail")
	}
	if _, statErr := os.Stat(dst + ".part"); statErr != nil {
		t.Fatalf(".part should persist after a break: %v", statErr)
	}

	// Run 2: resume — only the missing chunks (2,3) are fetched.
	fake.failChunk = 0
	fake.mu.Lock()
	fake.getCalls = 0
	fake.mu.Unlock()
	var resumeAlready, resumeTotal int64
	n, err := resumableDownload(context.Background(), fake, &resumableDownloadOptions{
		Bucket: "b", Key: "k", DestPath: dst, PartSize: 1024, Concurrency: 1,
		OnResume: func(already, total int64) { resumeAlready, resumeTotal = already, total },
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumeAlready != 1024 || resumeTotal != int64(len(content)) {
		t.Errorf("OnResume = (%d, %d), want (1024, %d) — one chunk already on disk", resumeAlready, resumeTotal, len(content))
	}
	if n != int64(len(content)) {
		t.Errorf("size = %d, want %d", n, len(content))
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, content) {
		t.Error("resumed file does not match the source content")
	}
	if c := fake.calls(); c != 2 {
		t.Errorf("resume GetObject calls = %d, want 2 (only chunks 2,3)", c)
	}
}
