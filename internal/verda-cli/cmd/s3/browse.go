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
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// rowKind tags each browser row so a Select index maps back to an action.
type rowKind int

const (
	rowUp rowKind = iota
	rowExit
	rowFolder
	rowObject
	rowDownloadMulti
	rowDeleteMulti
)

type browseRow struct {
	kind  rowKind
	label string
	value string // folder: full prefix; object: full key
	size  int64
}

// runLsBrowser is the interactive S3 explorer launched by `verda s3 ls` with no
// argument on a terminal. It walks buckets -> prefixes -> objects (one
// ListObjectsV2 delimiter level at a time) and offers per-object actions
// (download / info / delete). Esc ascends one level; Ctrl+C exits.
func runLsBrowser(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) error {
	// Best-effort prune of stale download checkpoints + shared lock files so
	// download-only users (who never hit the upload-path GC) don't accumulate them.
	_ = gcDownloadCheckpoints(0)
	_ = gcCheckpoints(0)

	cur := URI{} // empty Bucket == root (the bucket list)
	for {
		if cur.Bucket == "" {
			next, exit, err := browseBuckets(ctx, f, ioStreams, client)
			if err != nil || exit {
				return err
			}
			if next != "" {
				cur = URI{Bucket: next}
			}
			continue
		}

		again, err := browseLevel(ctx, f, ioStreams, client, &cur)
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}
}

// browseBuckets shows the bucket list. Returns (chosen bucket, exit, err);
// exit is true when the user chose Exit / Ctrl+C / Esc at the root.
func browseBuckets(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) (bucket string, exit bool, err error) {
	out, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading buckets...", func() (*s3.ListBucketsOutput, error) {
		return client.ListBuckets(ctx, &s3.ListBucketsInput{})
	})
	if err != nil {
		return "", true, translateError(err)
	}
	if len(out.Buckets) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No buckets found.")
		return "", true, nil
	}

	labels := make([]string, 0, len(out.Buckets)+1)
	for i := range out.Buckets {
		labels = append(labels, "📦 "+aws.ToString(out.Buckets[i].Name))
	}
	labels = append(labels, "Exit")

	idx, err := f.Prompter().Select(ctx, "Select bucket", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", true, nil
		}
		return "", true, err
	}
	if idx == len(out.Buckets) { // Exit
		return "", true, nil
	}
	return aws.ToString(out.Buckets[idx].Name), false, nil
}

// browseLevel lists one delimiter level under cur and handles the selection.
// Returns again=false to leave the browser entirely; again=true to keep
// looping (cur may have been mutated to drill in/out).
func browseLevel(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, cur *URI) (bool, error) {
	payload, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading...", func() (objectsPayload, error) {
		return collectObjects(ctx, f, ioStreams, client, *cur, "/")
	})
	if err != nil {
		return false, err
	}

	rows := buildBrowseRows(*cur, payload)
	labels := make([]string, len(rows))
	for i := range rows {
		labels[i] = rows[i].label
	}

	idx, err := f.Prompter().Select(ctx, browseBreadcrumb(*cur), labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptInterrupt(err) {
			return false, nil // Ctrl+C exits the browser
		}
		if cmdutil.IsPromptBack(err) {
			ascend(cur) // Esc goes up one level
			return true, nil
		}
		return false, err
	}

	switch row := rows[idx]; row.kind {
	case rowUp:
		ascend(cur)
	case rowExit:
		return false, nil
	case rowFolder:
		cur.Key = row.value
	case rowDownloadMulti:
		exit, err := browseDownloadMulti(ctx, f, ioStreams, client, *cur, payload)
		if err != nil {
			return false, err
		}
		if exit {
			return false, nil
		}
	case rowObject:
		exit, err := objectActionMenu(ctx, f, ioStreams, client, URI{Bucket: cur.Bucket, Key: row.value}, row.size)
		if err != nil {
			return false, err
		}
		if exit {
			return false, nil
		}
	}
	return true, nil
}

// buildBrowseRows orders the rows: up, [download-multi], folders, objects, exit.
func buildBrowseRows(cur URI, payload objectsPayload) []browseRow {
	objRows := make([]browseRow, 0, len(payload.Objects))
	for i := range payload.Objects {
		o := &payload.Objects[i]
		name := relName(cur.Key, o.Key)
		if name == "" {
			continue // the prefix placeholder object, if any
		}
		objRows = append(objRows, browseRow{
			kind:  rowObject,
			label: fmt.Sprintf("📄 %-40s  %10s  %s", name, humanBytes(o.Size), o.Modified.UTC().Format(timestampLayout)),
			value: o.Key,
			size:  o.Size,
		})
	}

	rows := make([]browseRow, 0, len(payload.CommonPrefixes)+len(objRows)+3)
	rows = append(rows, browseRow{kind: rowUp, label: "↑  .."})
	if len(objRows) > 0 {
		rows = append(rows, browseRow{kind: rowDownloadMulti, label: "⬇  Download files here…"})
	}
	for _, p := range payload.CommonPrefixes {
		rows = append(rows, browseRow{kind: rowFolder, label: "📁 " + relName(cur.Key, p), value: p})
	}
	rows = append(rows, objRows...)
	rows = append(rows, browseRow{kind: rowExit, label: "Exit"})
	return rows
}

// browseBreadcrumb renders the current path as the Select title.
func browseBreadcrumb(cur URI) string {
	if cur.Key == "" {
		return "s3://" + cur.Bucket + "/"
	}
	return "s3://" + cur.Bucket + "/" + cur.Key
}

// ascend moves cur up one prefix level; at the bucket root it clears the bucket
// (returning to the bucket list).
func ascend(cur *URI) {
	if cur.Key == "" {
		cur.Bucket = ""
		return
	}
	trimmed := strings.TrimSuffix(cur.Key, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		cur.Key = trimmed[:i+1]
	} else {
		cur.Key = ""
	}
}

// relName returns the segment of full beneath prefix (the display name), with
// any trailing slash preserved for folders.
func relName(prefix, full string) string {
	return strings.TrimPrefix(full, prefix)
}

// objectActionMenu presents the per-object actions (Download / Info / Delete).
// Returns exit=true when the user chose to leave the browser after the action.
func objectActionMenu(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI, size int64) (bool, error) {
	name := path.Base(obj.Key)
	const (
		actDownload = iota
		actInfo
		actDelete
		actBack
	)
	labels := []string{"Download", "Info", "Delete", "← Back"}
	idx, err := f.Prompter().Select(ctx, fmt.Sprintf("%s (%s)", name, humanBytes(size)), labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	switch idx {
	case actDownload:
		return browseDownload(ctx, f, ioStreams, client, obj)
	case actInfo:
		return false, browseInfo(ctx, f, ioStreams, client, obj)
	case actDelete:
		return false, browseDelete(ctx, f, ioStreams, client, obj)
	default:
		return false, nil
	}
}

// browseDownload downloads one object to the user's Downloads folder via the
// resumable downloader, so re-selecting Download on an interrupted object
// resumes from its .part file. Pauses on a Back/Exit gate after completion so
// the summary stays on screen instead of snapping back to the list.
func browseDownload(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI) (bool, error) {
	dir, derr := defaultDownloadDir()
	if derr != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  note: %v — saving to the current directory\n", derr)
	}
	local := resolveLocalDest(dir, filepath.Base(obj.Key), obj.Bucket, obj.Key, map[string]bool{})
	announceRename(ioStreams, obj.Key, local)
	n, rate, err := downloadToLocal(ctx, f, ioStreams, client, obj, local, 0, defaultConcurrency, false, false)
	if err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "✓ downloaded %s -> %s (%s)%s\n", obj.String(), absOrSelf(local), humanBytes(n), rate)
	return cmdutil.PromptBackOrExit(ctx, f.Prompter())
}

// browseDownloadMulti multi-selects objects at the current level and downloads
// the ticked set to the user's Downloads folder. Selection is scoped to ONE
// folder by construction — only the current level's direct objects are listed,
// subfolders are non-selectable drill-in entries — so the picked set never spans
// folders. Each file is placed via resolveLocalDest so an existing local file of
// the same name is renamed rather than overwritten. Non-destructive, so no
// confirmation. Pauses on a Back/Exit gate after the summary so it stays on screen.
func browseDownloadMulti(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, cur URI, payload objectsPayload) (bool, error) {
	var objs []objectEntry
	var labels []string
	for i := range payload.Objects {
		name := relName(cur.Key, payload.Objects[i].Key)
		if name == "" {
			continue
		}
		objs = append(objs, payload.Objects[i])
		labels = append(labels, fmt.Sprintf("%s  (%s)", name, humanBytes(payload.Objects[i].Size)))
	}
	if len(objs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No files to download at this level.")
		return false, nil
	}

	idxs, err := f.Prompter().MultiSelect(ctx, "Select files to download (space to toggle)", labels, tui.WithMultiSelectShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	if len(idxs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Nothing selected.")
		return false, nil
	}

	dir, derr := defaultDownloadDir()
	if derr != nil {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  note: %v — saving to the current directory\n", derr)
	}
	used := map[string]bool{}
	var total int64
	for _, ix := range idxs {
		obj := URI{Bucket: cur.Bucket, Key: objs[ix].Key}
		local := resolveLocalDest(dir, filepath.Base(obj.Key), obj.Bucket, obj.Key, used)
		announceRename(ioStreams, obj.Key, local)
		n, rate, dlErr := downloadToLocal(ctx, f, ioStreams, client, obj, local, 0, defaultConcurrency, false, false)
		if dlErr != nil {
			return false, fmt.Errorf("downloading %s: %w", obj.String(), dlErr)
		}
		total += n
		_, _ = fmt.Fprintf(ioStreams.Out, "✓ downloaded %s -> %s (%s)%s\n", obj.String(), absOrSelf(local), humanBytes(n), rate)
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Downloaded %d file(s), %s total -> %s\n", len(idxs), humanBytes(total), absOrSelf(dir))
	return cmdutil.PromptBackOrExit(ctx, f.Prompter())
}

// defaultDownloadDir returns the user's Downloads folder (created if needed) for
// interactive browser downloads. On failure it returns "." (the current
// directory) plus a non-nil reason so the caller can tell the user where the
// file actually landed. cp uses its explicit destination argument instead.
func defaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".", errors.New("could not resolve your home directory")
	}
	dir := filepath.Join(home, "Downloads")
	if mkErr := os.MkdirAll(dir, 0o750); mkErr != nil {
		return ".", fmt.Errorf("could not use %s: %w", dir, mkErr)
	}
	return dir, nil
}

// resolveLocalDest picks the local path for downloading obj into dir without
// clobbering an unrelated local file, while still allowing a genuine resume.
// It returns dir/<base> when that name is free OR is an in-progress resume of
// THIS object (its .part + checkpoint match); otherwise it appends "-2", "-3", …
// before the extension until it finds a name that is neither an existing file
// nor a foreign .part. used tracks names already handed out in the same batch.
func resolveLocalDest(dir, base, bucket, key string, used map[string]bool) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = stem + "-" + strconv.Itoa(i+1) + ext
		}
		if used[name] {
			continue
		}
		full := filepath.Join(dir, name)
		if fileExists(full + ".part") {
			if hasResumableDownload(full, bucket, key) {
				used[name] = true
				return full // our interrupted download → resume into it
			}
			continue // a foreign partial download owns this name → don't clobber
		}
		if fileExists(full) {
			continue // unrelated completed file → don't overwrite
		}
		used[name] = true
		return full
	}
}

// announceRename notes when resolveLocalDest had to pick a non-default filename
// to avoid overwriting an existing local file. A resume (name unchanged) is
// silent; the downloader prints its own "Resuming…" line.
func announceRename(ioStreams cmdutil.IOStreams, key, local string) {
	if want := path.Base(key); filepath.Base(local) != want {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s already exists locally — saving as %s\n", want, filepath.Base(local))
	}
}

// browseInfo prints object metadata via HeadObject.
func browseInfo(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI) error {
	head, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading details...", func() (*s3.HeadObjectOutput, error) {
		return client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(obj.Bucket), Key: aws.String(obj.Key)})
	})
	if err != nil {
		return translateError(err)
	}
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	w := ioStreams.Out
	_, _ = fmt.Fprintf(w, "\n  %s\n", label.Render(obj.String()))
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Size"), humanBytes(aws.ToInt64(head.ContentLength)))
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Modified"), aws.ToTime(head.LastModified).UTC().Format(timestampLayout))
	_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("ETag"), aws.ToString(head.ETag))
	if ct := aws.ToString(head.ContentType); ct != "" {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Type"), ct)
	}
	if sc := string(head.StorageClass); sc != "" {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", label.Render("Storage"), sc)
	}
	_, _ = fmt.Fprintln(w)
	return nil
}

// browseDelete confirms then deletes a single object (reusing the destructive
// red-warning + prompter.Confirm convention).
func browseDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI) error {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", warn.Render("This will permanently delete "+obj.String()))
	confirmed, err := f.Prompter().Confirm(ctx, fmt.Sprintf("Delete %s?", obj.String()))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil
		}
		return err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(obj.Bucket), Key: aws.String(obj.Key)}); err != nil {
		return translateError(err)
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "✓ deleted %s\n", obj.String())
	return nil
}
