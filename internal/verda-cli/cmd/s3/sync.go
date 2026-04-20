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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// syncOptions captures the flags accepted by `verda s3 sync`.
type syncOptions struct {
	Delete          bool
	ExactTimestamps bool
	Include         []string
	Exclude         []string
	Dryrun          bool
}

// syncEntry represents a single item in a source or destination inventory,
// keyed by a normalised relative path (forward slashes). Sync compares
// entries on size + modified time.
type syncEntry struct {
	Key      string
	Size     int64
	Modified time.Time
}

// syncSummary is the aggregate footer for structured output.
type syncSummary struct {
	Files      int   `json:"files"       yaml:"files"`
	Bytes      int64 `json:"bytes"       yaml:"bytes"`
	DurationMs int64 `json:"duration_ms" yaml:"duration_ms"`
}

// syncPayload is the JSON/YAML shape emitted by the sync command.
type syncPayload struct {
	Copied  []transferEntry `json:"copied"  yaml:"copied"`
	Deleted []string        `json:"deleted" yaml:"deleted"`
	Skipped int             `json:"skipped" yaml:"skipped"`
	Dryrun  bool            `json:"dryrun"  yaml:"dryrun"`
	Summary syncSummary     `json:"summary" yaml:"summary"`
}

// NewCmdSync builds the `verda s3 sync` cobra command.
func NewCmdSync(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &syncOptions{}

	cmd := &cobra.Command{
		Use:   "sync <src> <dst>",
		Short: "Incrementally sync a directory/prefix with S3",
		Long: cmdutil.LongDesc(`
			Recursively sync files between a local directory and an S3
			prefix, between two S3 prefixes, or in either direction. Only
			new or updated files are copied; unchanged files are skipped.

			By default a file is considered "updated" when the source is
			newer OR the sizes differ. Pass --exact-timestamps to require
			the timestamps match exactly (any difference triggers a copy).

			With --delete, files present at the destination but not at the
			source are removed after the copy phase. Include/exclude glob
			filters are applied to the relative path on BOTH sides — an
			excluded key is neither copied nor deleted.

			With --dryrun, the planned copies/deletes are listed but no SDK
			calls or local filesystem changes are made.
		`),
		Example: cmdutil.Examples(`
			# Mirror a local directory to S3
			verda s3 sync ./site s3://my-bucket/site/

			# Pull a prefix down incrementally
			verda s3 sync s3://my-bucket/backups/ ./backups

			# Copy between buckets, mirroring deletions
			verda s3 sync s3://src/data/ s3://dst/data/ --delete

			# Only sync .csv files, preview first
			verda s3 sync ./data s3://my-bucket/data/ --include "*.csv" --dryrun
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, f, ioStreams, opts, args[0], args[1])
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Delete, "delete", false, "Delete destination files that have no corresponding source")
	flags.BoolVar(&opts.ExactTimestamps, "exact-timestamps", false, "Require exact timestamp match instead of src-newer")
	flags.StringArrayVar(&opts.Include, "include", nil, "Only sync entries matching this glob (repeatable)")
	flags.StringArrayVar(&opts.Exclude, "exclude", nil, "Skip entries matching this glob (repeatable, overrides --include)")
	flags.BoolVar(&opts.Dryrun, "dryrun", false, "Preview planned copies/deletes without performing them")

	return cmd
}

func runSync(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *syncOptions, srcArg, dstArg string) error {
	dir := detectDirection(srcArg, dstArg)
	if dir == dirInvalid {
		return cmdutil.UsageErrorf(cmd, "sync requires at least one s3:// URI")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	switch dir {
	case dirUpload:
		dstURI, err := ParseS3URI(dstArg)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runSyncUpload(ctx, f, ioStreams, srcArg, dstURI, opts)
	case dirDownload:
		srcURI, err := ParseS3URI(srcArg)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runSyncDownload(ctx, f, ioStreams, srcURI, dstArg, opts)
	case dirCopy:
		srcURI, err := ParseS3URI(srcArg)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		dstURI, err := ParseS3URI(dstArg)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runSyncCopy(ctx, f, ioStreams, srcURI, dstURI, opts)
	}
	return nil
}

// ----- Local -> S3 --------------------------------------------------------

func runSyncUpload(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, srcDir string, dst URI, opts *syncOptions) error {
	info, err := os.Stat(srcDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("sync source must be a directory: %s", srcDir)
	}

	transporter, err := transporterBuilder(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}
	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	srcEntries, err := enumerateLocal(srcDir, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}
	dstEntries, err := enumerateS3(ctx, f, ioStreams, apiClient, dst.Bucket, dst.Key, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}

	plan := planSync(srcEntries, dstEntries, opts.ExactTimestamps)

	payload := newSyncPayload(opts.Dryrun)
	payload.Skipped = plan.skipped
	started := time.Now()
	cpOpts := &cpOptions{Dryrun: opts.Dryrun}

	// Copies: upload each planned entry.
	for _, rel := range plan.toCopy {
		localPath := filepath.Join(srcDir, filepath.FromSlash(rel))
		key := joinKey(dst.Key, rel)
		sub := newCpPayload(opts.Dryrun)
		quiet := quietStreams(ioStreams)
		if err := uploadOne(ctx, f, quiet, transporter, localPath, URI{Bucket: dst.Bucket, Key: key}, rel, cpOpts, &sub); err != nil {
			return err
		}
		appendCopied(ioStreams, f, payload, sub, rel, opts.Dryrun, "copied")
	}

	// Deletes: remote destination keys with no source counterpart.
	if opts.Delete {
		for _, rel := range plan.toDelete {
			if opts.Dryrun {
				emitDryrunDelete(ioStreams, f, payload, rel, fmt.Sprintf("s3://%s/%s", dst.Bucket, joinKey(dst.Key, rel)))
				continue
			}
			key := joinKey(dst.Key, rel)
			in := &s3.DeleteObjectInput{Bucket: aws.String(dst.Bucket), Key: aws.String(key)}
			if _, derr := apiClient.DeleteObject(ctx, in); derr != nil {
				return translateError(derr)
			}
			appendDeleted(ioStreams, f, payload, rel)
		}
	}

	return finalizeSync(ioStreams, f, payload, started, opts.Dryrun)
}

// ----- S3 -> Local --------------------------------------------------------

func runSyncDownload(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src URI, dstDir string, opts *syncOptions) error {
	// Ensure the destination directory exists so enumerateLocal doesn't error
	// out on the first sync into a brand-new dir.
	if err := os.MkdirAll(dstDir, 0o750); err != nil {
		return err
	}

	transporter, err := transporterBuilder(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}
	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	srcEntries, err := enumerateS3(ctx, f, ioStreams, apiClient, src.Bucket, src.Key, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}
	dstEntries, err := enumerateLocal(dstDir, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}

	plan := planSync(srcEntries, dstEntries, opts.ExactTimestamps)

	payload := newSyncPayload(opts.Dryrun)
	payload.Skipped = plan.skipped
	started := time.Now()
	cpOpts := &cpOptions{Dryrun: opts.Dryrun}

	for _, rel := range plan.toCopy {
		key := joinKey(src.Key, rel)
		localPath, joinErr := safeJoin(dstDir, rel)
		if joinErr != nil {
			return joinErr
		}
		sub := newCpPayload(opts.Dryrun)
		quiet := quietStreams(ioStreams)
		if err := downloadOne(ctx, f, quiet, transporter, URI{Bucket: src.Bucket, Key: key}, localPath, rel, cpOpts, &sub); err != nil {
			return err
		}
		appendCopied(ioStreams, f, payload, sub, rel, opts.Dryrun, "downloaded")
	}

	if opts.Delete {
		for _, rel := range plan.toDelete {
			if opts.Dryrun {
				emitDryrunDelete(ioStreams, f, payload, rel, filepath.Join(dstDir, filepath.FromSlash(rel)))
				continue
			}
			localPath, joinErr := safeJoin(dstDir, rel)
			if joinErr != nil {
				return joinErr
			}
			if rerr := os.Remove(localPath); rerr != nil {
				return fmt.Errorf("remove %s: %w", localPath, rerr)
			}
			appendDeleted(ioStreams, f, payload, rel)
		}
	}

	return finalizeSync(ioStreams, f, payload, started, opts.Dryrun)
}

// ----- S3 -> S3 -----------------------------------------------------------

func runSyncCopy(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src, dst URI, opts *syncOptions) error {
	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	srcEntries, err := enumerateS3(ctx, f, ioStreams, apiClient, src.Bucket, src.Key, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}
	dstEntries, err := enumerateS3(ctx, f, ioStreams, apiClient, dst.Bucket, dst.Key, opts.Include, opts.Exclude)
	if err != nil {
		return err
	}

	plan := planSync(srcEntries, dstEntries, opts.ExactTimestamps)

	payload := newSyncPayload(opts.Dryrun)
	payload.Skipped = plan.skipped
	started := time.Now()
	cpOpts := &cpOptions{Dryrun: opts.Dryrun}

	for _, rel := range plan.toCopy {
		srcKey := joinKey(src.Key, rel)
		dstKey := joinKey(dst.Key, rel)
		sub := newCpPayload(opts.Dryrun)
		quiet := quietStreams(ioStreams)
		if err := copyOne(ctx, f, quiet, apiClient,
			URI{Bucket: src.Bucket, Key: srcKey},
			URI{Bucket: dst.Bucket, Key: dstKey},
			rel, cpOpts, &sub); err != nil {
			return err
		}
		appendCopied(ioStreams, f, payload, sub, rel, opts.Dryrun, "copied")
	}

	if opts.Delete {
		for _, rel := range plan.toDelete {
			if opts.Dryrun {
				emitDryrunDelete(ioStreams, f, payload, rel, fmt.Sprintf("s3://%s/%s", dst.Bucket, joinKey(dst.Key, rel)))
				continue
			}
			key := joinKey(dst.Key, rel)
			in := &s3.DeleteObjectInput{Bucket: aws.String(dst.Bucket), Key: aws.String(key)}
			if _, derr := apiClient.DeleteObject(ctx, in); derr != nil {
				return translateError(derr)
			}
			appendDeleted(ioStreams, f, payload, rel)
		}
	}

	return finalizeSync(ioStreams, f, payload, started, opts.Dryrun)
}

// ----- Enumerators --------------------------------------------------------

// enumerateLocal walks root and returns one syncEntry per regular file whose
// relative path (forward-slash form) satisfies the include/exclude filters.
// The returned slice is sorted by Key for deterministic iteration.
func enumerateLocal(root string, includes, excludes []string) ([]syncEntry, error) {
	var entries []syncEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// If the tree doesn't exist yet (first sync), treat as empty.
			if os.IsNotExist(walkErr) && path == root {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !matchFilters(rel, includes, excludes) {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		entries = append(entries, syncEntry{
			Key:      rel,
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// enumerateS3 paginates ListObjectsV2 under prefix and returns one syncEntry
// per object whose relative path (prefix stripped) satisfies the
// include/exclude filters. The returned slice is sorted by Key.
func enumerateS3(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, bucket, prefix string, includes, excludes []string) ([]syncEntry, error) {
	var (
		entries []syncEntry
		token   *string
	)
	for {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(bucket)}
		if prefix != "" {
			in.Prefix = aws.String(prefix)
		}
		if token != nil {
			in.ContinuationToken = token
		}
		out, err := client.ListObjectsV2(ctx, in)
		if err != nil {
			return nil, translateError(err)
		}
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(),
			fmt.Sprintf("ListObjectsV2 response: %d object(s)", len(out.Contents)), out)

		for i := range out.Contents {
			key := aws.ToString(out.Contents[i].Key)
			rel := relKey(prefix, key)
			if rel == "" {
				// A marker at the prefix itself — ignore.
				continue
			}
			if !matchFilters(rel, includes, excludes) {
				continue
			}
			entries = append(entries, syncEntry{
				Key:      rel,
				Size:     aws.ToInt64(out.Contents[i].Size),
				Modified: aws.ToTime(out.Contents[i].LastModified),
			})
		}
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			break
		}
		token = out.NextContinuationToken
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// ----- Planning -----------------------------------------------------------

// syncPlan is the decomposed result of comparing two inventories.
type syncPlan struct {
	toCopy   []string // relPaths to copy from src -> dst
	toDelete []string // relPaths to delete from dst (only when --delete)
	skipped  int      // entries that match and need no work
}

// planSync compares srcEntries against dstEntries and returns the set of
// relPaths to copy vs delete, plus a count of unchanged entries. All slices
// are sorted for deterministic iteration (the enumerators return sorted
// inputs already).
func planSync(srcEntries, dstEntries []syncEntry, exactTimestamps bool) syncPlan {
	dstIdx := make(map[string]syncEntry, len(dstEntries))
	for _, e := range dstEntries {
		dstIdx[e.Key] = e
	}
	srcSet := make(map[string]struct{}, len(srcEntries))

	var plan syncPlan
	for _, se := range srcEntries {
		srcSet[se.Key] = struct{}{}
		de, present := dstIdx[se.Key]
		if !present {
			plan.toCopy = append(plan.toCopy, se.Key)
			continue
		}
		if needsCopy(se, de, exactTimestamps) {
			plan.toCopy = append(plan.toCopy, se.Key)
			continue
		}
		plan.skipped++
	}

	for _, de := range dstEntries {
		if _, ok := srcSet[de.Key]; !ok {
			plan.toDelete = append(plan.toDelete, de.Key)
		}
	}
	sort.Strings(plan.toCopy)
	sort.Strings(plan.toDelete)
	return plan
}

// needsCopy reports whether src differs enough from dst to require a copy.
// Default semantics: "src is newer, or sizes differ". With exactTimestamps:
// "timestamps differ at all, or sizes differ".
func needsCopy(src, dst syncEntry, exactTimestamps bool) bool {
	if src.Size != dst.Size {
		return true
	}
	if exactTimestamps {
		return !src.Modified.Equal(dst.Modified)
	}
	return src.Modified.After(dst.Modified)
}

// ----- Output helpers -----------------------------------------------------

func newSyncPayload(dryrun bool) *syncPayload {
	return &syncPayload{
		Copied:  []transferEntry{},
		Deleted: []string{},
		Dryrun:  dryrun,
	}
}

// appendCopied merges the result of a single uploadOne/downloadOne/copyOne
// invocation into the aggregate sync payload and emits the human line.
func appendCopied(ioStreams cmdutil.IOStreams, f cmdutil.Factory, payload *syncPayload, sub cpPayload, rel string, dryrun bool, _ string) {
	structured := isStructured(f.OutputFormat())
	if len(sub.Transfers) == 0 {
		return
	}
	entry := sub.Transfers[len(sub.Transfers)-1]
	payload.Copied = append(payload.Copied, entry)
	if structured {
		return
	}
	if dryrun {
		_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would copy %s\n", rel)
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 copied %s (%s)\n", rel, humanBytes(entry.Bytes))
}

func appendDeleted(ioStreams cmdutil.IOStreams, f cmdutil.Factory, payload *syncPayload, rel string) {
	payload.Deleted = append(payload.Deleted, rel)
	if isStructured(f.OutputFormat()) {
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "- removed %s\n", rel)
}

// emitDryrunDelete adds a delete to the payload and prints a human preview
// line, without performing the delete. displayPath is the user-facing target
// (e.g. "s3://bucket/key" or a local path).
func emitDryrunDelete(ioStreams cmdutil.IOStreams, f cmdutil.Factory, payload *syncPayload, rel, displayPath string) {
	payload.Deleted = append(payload.Deleted, rel)
	if isStructured(f.OutputFormat()) {
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would delete %s\n", displayPath)
}

// quietStreams returns an IOStreams that silences Out so the per-file lines
// emitted by uploadOne/downloadOne/copyOne don't appear — sync emits its own
// "copied" line per entry.
func quietStreams(ioStreams cmdutil.IOStreams) cmdutil.IOStreams {
	return cmdutil.IOStreams{In: ioStreams.In, Out: discardWriter{}, ErrOut: ioStreams.ErrOut}
}

// finalizeSync tallies the payload summary, emits structured output if
// requested, and otherwise prints the human-readable footer.
func finalizeSync(ioStreams cmdutil.IOStreams, f cmdutil.Factory, payload *syncPayload, started time.Time, dryrun bool) error {
	var bytesTotal int64
	for _, t := range payload.Copied {
		bytesTotal += t.Bytes
	}
	elapsed := time.Since(started)
	payload.Summary = syncSummary{
		Files:      len(payload.Copied),
		Bytes:      bytesTotal,
		DurationMs: elapsed.Milliseconds(),
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	verb := "elapsed"
	if dryrun {
		verb = "planned"
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "%d copied, %d skipped, %d deleted, %s %s\n",
		len(payload.Copied), payload.Skipped, len(payload.Deleted),
		elapsed.Truncate(time.Millisecond), verb)
	return nil
}
