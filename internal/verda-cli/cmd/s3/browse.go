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
	"path"
	"path/filepath"
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
func browseBuckets(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) (string, bool, error) {
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
		if err := browseDownloadMulti(ctx, f, ioStreams, client, *cur, payload); err != nil {
			return false, err
		}
	case rowObject:
		if err := objectActionMenu(ctx, f, ioStreams, client, URI{Bucket: cur.Bucket, Key: row.value}, row.size); err != nil {
			return false, err
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
func objectActionMenu(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI, size int64) error {
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
			return nil
		}
		return err
	}
	switch idx {
	case actDownload:
		return browseDownload(ctx, f, ioStreams, client, obj)
	case actInfo:
		return browseInfo(ctx, f, ioStreams, client, obj)
	case actDelete:
		return browseDelete(ctx, f, ioStreams, client, obj)
	default:
		return nil
	}
}

// browseDownload downloads one object to ./<basename> via the resumable
// downloader, so re-selecting Download on an interrupted object resumes from
// its .part file.
func browseDownload(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI) error {
	local := filepath.Base(obj.Key)
	n, rate, err := downloadToLocal(ctx, f, ioStreams, client, obj, local, 0, defaultConcurrency, false, false)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "✓ downloaded %s -> %s (%s)%s\n", obj.String(), absOrSelf(local), humanBytes(n), rate)
	return nil
}

// browseDownloadMulti multi-selects objects at the current level and downloads
// the ticked set to the cwd. Objects only (folders are not selectable in v1);
// non-destructive, so no confirmation.
func browseDownloadMulti(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, cur URI, payload objectsPayload) error {
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
		return nil
	}

	idxs, err := f.Prompter().MultiSelect(ctx, "Select files to download (space to toggle)", labels, tui.WithMultiSelectShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil
		}
		return err
	}
	if len(idxs) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Nothing selected.")
		return nil
	}

	var total int64
	for _, ix := range idxs {
		obj := URI{Bucket: cur.Bucket, Key: objs[ix].Key}
		n, rate, derr := downloadToLocal(ctx, f, ioStreams, client, obj, filepath.Base(obj.Key), 0, defaultConcurrency, false, false)
		if derr != nil {
			return fmt.Errorf("downloading %s: %w", obj.String(), derr)
		}
		total += n
		_, _ = fmt.Fprintf(ioStreams.Out, "✓ downloaded %s -> %s (%s)%s\n", obj.String(), absOrSelf(filepath.Base(obj.Key)), humanBytes(n), rate)
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Downloaded %d file(s), %s total\n", len(idxs), humanBytes(total))
	return nil
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
