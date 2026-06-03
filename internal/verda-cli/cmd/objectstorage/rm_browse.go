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

	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// runRmBrowser is the interactive deleter launched by `verda object-storage rm` with no
// argument on a terminal. It shares the download browser's folder navigation
// (buckets -> prefixes -> objects, one delimiter level at a time) but each level
// offers a multi-select delete instead of download. Esc ascends a level; Ctrl+C
// exits. Deletes still go through the confirm + executeRm path, so the red
// warning + preview are identical to the flag-driven `rm`.
func runRmBrowser(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API) error {
	cur := URI{} // empty Bucket == the bucket list
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

		again, err := rmBrowseLevel(ctx, f, ioStreams, client, &cur)
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}
}

// rmBrowseLevel lists one delimiter level under cur and handles the selection.
// Returns again=false to leave the browser entirely; again=true to keep looping
// (cur may have been mutated to drill in/out).
func rmBrowseLevel(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, cur *URI) (bool, error) {
	payload, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading...", func() (objectsPayload, error) {
		return collectObjects(ctx, f, ioStreams, client, *cur, "/")
	})
	if err != nil {
		return false, err
	}

	rows := buildRmRows(*cur, payload)
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
	case rowDeleteMulti:
		if err := rmBrowseDeleteMulti(ctx, f, ioStreams, client, *cur, payload); err != nil {
			return false, err
		}
	case rowObject:
		if err := rmBrowseDeleteOne(ctx, f, ioStreams, client, URI{Bucket: cur.Bucket, Key: row.value}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// buildRmRows orders the rows: up, [delete-multi], folders, objects, exit.
func buildRmRows(cur URI, payload objectsPayload) []browseRow {
	objRows := make([]browseRow, 0, len(payload.Objects))
	for i := range payload.Objects {
		o := &payload.Objects[i]
		name := relName(cur.Key, o.Key)
		if name == "" {
			continue // the prefix placeholder object, if any
		}
		objRows = append(objRows, browseRow{
			kind:  rowObject,
			label: fmt.Sprintf("🗑  %-40s  %10s  %s", name, humanBytes(o.Size), o.Modified.UTC().Format(timestampLayout)),
			value: o.Key,
			size:  o.Size,
		})
	}

	rows := make([]browseRow, 0, len(payload.CommonPrefixes)+len(objRows)+3)
	rows = append(rows, browseRow{kind: rowUp, label: "↑  .."})
	if len(objRows) > 0 {
		rows = append(rows, browseRow{kind: rowDeleteMulti, label: "🗑  Delete files here…"})
	}
	for _, p := range payload.CommonPrefixes {
		rows = append(rows, browseRow{kind: rowFolder, label: "📁 " + relName(cur.Key, p), value: p})
	}
	rows = append(rows, objRows...)
	rows = append(rows, browseRow{kind: rowExit, label: "Exit"})
	return rows
}

// rmBrowseDeleteMulti multi-selects objects at the current level and deletes the
// ticked set through the shared confirm + executeRm path. Objects only (folders
// are drilled into, not bulk-deleted here).
func rmBrowseDeleteMulti(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, cur URI, payload objectsPayload) error {
	keys := make([]string, 0, len(payload.Objects))
	labels := make([]string, 0, len(payload.Objects))
	for i := range payload.Objects {
		name := relName(cur.Key, payload.Objects[i].Key)
		if name == "" {
			continue
		}
		keys = append(keys, payload.Objects[i].Key)
		labels = append(labels, fmt.Sprintf("%s  (%s)", name, humanBytes(payload.Objects[i].Size)))
	}
	if len(keys) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No files to delete at this level.")
		return nil
	}

	idxs, err := f.Prompter().MultiSelect(ctx, "Select files to delete (space to toggle)", labels, tui.WithMultiSelectShowHints(true))
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

	targets := make([]string, 0, len(idxs))
	for _, ix := range idxs {
		targets = append(targets, keys[ix])
	}
	return confirmAndDelete(ctx, f, ioStreams, client, URI{Bucket: cur.Bucket, Key: cur.Key}, targets, true)
}

// rmBrowseDeleteOne deletes a single chosen object via the shared confirm path.
func rmBrowseDeleteOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, obj URI) error {
	return confirmAndDelete(ctx, f, ioStreams, client, obj, []string{obj.Key}, false)
}

// confirmAndDelete runs the red-warning confirm and, on approval, the delete.
// A clean cancel (Esc/Ctrl+C/no) returns nil so the browser stays open.
func confirmAndDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI, targets []string, recursive bool) error {
	confirmed, cerr := confirmRm(ctx, f, ioStreams, uri, targets, recursive)
	if cerr != nil {
		if cmdutil.IsPromptCancel(cerr) {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
		return cerr
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}
	return executeRm(ctx, f, ioStreams, client, uri, targets, recursive)
}
