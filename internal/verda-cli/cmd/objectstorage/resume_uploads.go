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
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// inferPartSize recovers the original part size from the server's existing
// parts: every full part is the same size, so the largest is the part size.
// Returns 0 when no parts are present (caller falls back to the auto size).
func inferPartSize(parts []s3types.Part) int64 {
	var largest int64
	for i := range parts {
		largest = max(largest, aws.ToInt64(parts[i].Size))
	}
	return largest
}

// findCheckpointByUploadID scans the local checkpoint store for one whose
// UploadID matches. Returns (nil, nil) when none is found (e.g. the upload was
// started on another machine or the checkpoint was pruned).
func findCheckpointByUploadID(uploadID string) (*checkpoint, error) {
	dir, err := checkpointDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for i := range entries {
		name := entries[i].Name()
		if entries[i].IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		cp, lerr := loadCheckpoint(strings.TrimSuffix(name, ".json"))
		if lerr != nil || cp == nil {
			continue
		}
		if cp.UploadID == uploadID {
			return cp, nil
		}
	}
	return nil, nil
}

// promptResumeFromUploads lets the user pick an in-progress upload and resume
// it. Each row is annotated with whether a local checkpoint was found; when it
// wasn't, the user is asked for the source file. ctx must be unbounded (the
// resume runs a full upload), so callers pass cmd.Context(), not a timeout ctx.
func promptResumeFromUploads(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket string, uploads []uploadEntry) error {
	checkpoints := make([]*checkpoint, len(uploads))
	labels := make([]string, 0, len(uploads)+1)
	for i := range uploads {
		cp, _ := findCheckpointByUploadID(uploads[i].UploadID)
		checkpoints[i] = cp
		mark := "(no local file — will prompt)"
		if cp != nil {
			mark = "✓ resumable"
		}
		labels = append(labels, fmt.Sprintf("%-44s  %9s  %s", uploads[i].Key, humanBytes(uploads[i].Size), mark))
	}
	labels = append(labels, "Exit")

	idx, err := f.Prompter().Select(ctx, "Select an upload to resume", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil
		}
		return err
	}
	if idx == len(uploads) { // Exit
		return nil
	}

	sel := uploads[idx]
	absPath := ""
	if cp := checkpoints[idx]; cp != nil {
		if _, statErr := os.Stat(cp.AbsPath); statErr == nil {
			absPath = cp.AbsPath
		}
	}
	if absPath == "" {
		absPath, err = promptResumeSource(ctx, f, ioStreams, sel.Key)
		if err != nil || absPath == "" {
			return err
		}
	}
	return resumeServerUpload(ctx, f, ioStreams, client, bucket, sel.Key, sel.UploadID, absPath)
}

// promptResumeSource asks for the local file backing an upload with no
// checkpoint, re-prompting until it exists. ("", nil) on cancel/empty.
func promptResumeSource(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, key string) (string, error) {
	for {
		p, err := f.Prompter().TextInput(ctx, "Local file for "+key)
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				return "", nil
			}
			return "", err
		}
		p = strings.TrimSpace(p)
		if p == "" {
			return "", nil
		}
		if _, statErr := os.Stat(p); statErr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %v — try again.\n", statErr)
			continue
		}
		return p, nil
	}
}

// resumeServerUpload resumes an in-progress multipart upload (bucket/key/
// uploadID) against the local file at absPath. It infers the original part size
// from the server's parts (so byte ranges align), verifies the file is large
// enough, seeds a checkpoint that ADOPTS the existing UploadId, then runs the
// normal resumable path (progress + same-host lock).
func resumeServerUpload(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket, key, uploadID, absPath string) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("source %q: %w", absPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("source %q is a directory; resume expects the original file", absPath)
	}

	parts, err := listAllParts(ctx, client, bucket, key, uploadID)
	if err != nil {
		if isNoSuchUpload(err) {
			return fmt.Errorf("upload %s no longer exists on the server (expired or aborted)", uploadID)
		}
		return translateError(err)
	}

	partSize := inferPartSize(parts)
	if partSize > 0 {
		var maxN int32
		for i := range parts {
			maxN = max(maxN, aws.ToInt32(parts[i].PartNumber))
		}
		if int64(maxN-1)*partSize >= info.Size() {
			return fmt.Errorf("local file %q (%s) is smaller than the in-progress upload — it does not match this object",
				absPath, humanBytes(info.Size()))
		}
	}

	storedPartSize := partSize
	if storedPartSize == 0 {
		storedPartSize = computePartSize(info.Size(), 0)
	}
	cpParts := make([]checkpointPart, 0, len(parts))
	for i := range parts {
		cpParts = append(cpParts, checkpointPart{N: aws.ToInt32(parts[i].PartNumber), ETag: aws.ToString(parts[i].ETag)})
	}
	identity := uploadIdentity(absPath, bucket, key)
	if err := saveCheckpoint(identity, &checkpoint{
		UploadID:  uploadID,
		Bucket:    bucket,
		Key:       key,
		AbsPath:   absPath,
		FileSize:  info.Size(),
		MTime:     info.ModTime(),
		PartSize:  storedPartSize,
		CreatedAt: time.Now().UTC(),
		Parts:     cpParts,
	}); err != nil {
		return err
	}

	ropts := &resumableOptions{
		AbsPath:     absPath,
		Bucket:      bucket,
		Key:         key,
		ContentType: inferContentType(absPath, ""),
		FileSize:    info.Size(),
		MTime:       info.ModTime(),
		PartSize:    partSize, // 0 -> uploader auto-sizes (only when no parts exist yet)
		Concurrency: defaultConcurrency,
	}
	return runResumable(ctx, f, ioStreams, client, ropts, path.Base(key))
}
