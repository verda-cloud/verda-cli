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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdMv builds the `verda s3 mv` cobra command. A move is a copy followed
// by a delete of the source, and is implemented by reusing the same transfer
// primitives the cp command uses. The source is only removed once the
// transfer portion has succeeded — if transfer fails for an entry, that
// entry's source is preserved.
func NewCmdMv(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &cpOptions{}

	cmd := &cobra.Command{
		Use:   "mv <src> <dst>",
		Short: "Move files between local and S3, or between S3 buckets",
		Long: cmdutil.LongDesc(`
			Move files between local paths and S3 URIs, or between two S3
			URIs. At least one of <src> or <dst> must be an s3:// URI. A
			move is a copy followed by a delete of the source — the source
			is only removed once the transfer succeeds.

			With --recursive, local moves walk the directory and S3 moves
			paginate through every object under the source prefix.
			--include and --exclude glob patterns filter the set (matched
			against the relative path; '*' does not cross '/').

			With --dryrun, the planned moves are listed but no SDK calls
			or local filesystem changes are made.
		`),
		Example: cmdutil.Examples(`
			# Move a single file to S3 (removes the local source on success)
			verda s3 mv ./report.csv s3://my-bucket/reports/report.csv

			# Move an object out of S3 to local disk
			verda s3 mv s3://my-bucket/report.csv ./report.csv

			# Move between buckets
			verda s3 mv s3://src/a.txt s3://dst/b.txt

			# Recursive move of every .csv file under a directory
			verda s3 mv ./data s3://my-bucket/data/ --recursive --include "*.csv"

			# Preview a recursive move
			verda s3 mv s3://my-bucket/logs/ ./logs --recursive --dryrun
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMv(cmd, f, ioStreams, opts, args[0], args[1])
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Recursive, "recursive", false, "Move every file under a directory/prefix")
	flags.StringArrayVar(&opts.Include, "include", nil, "Only move entries matching this glob (repeatable)")
	flags.StringArrayVar(&opts.Exclude, "exclude", nil, "Skip entries matching this glob (repeatable, overrides --include)")
	flags.BoolVar(&opts.Dryrun, "dryrun", false, "Preview moves without performing them")
	flags.StringVar(&opts.ContentType, "content-type", "", "Override Content-Type on uploads")

	return cmd
}

func runMv(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, src, dst string) error {
	dir := detectDirection(src, dst)
	if dir == dirInvalid {
		return cmdutil.UsageErrorf(cmd, "mv requires at least one s3:// URI")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	switch dir {
	case dirUpload:
		dstURI, err := ParseS3URI(dst)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runUploadMv(ctx, cmd, f, ioStreams, src, dstURI, opts)
	case dirDownload:
		srcURI, err := ParseS3URI(src)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runDownloadMv(ctx, cmd, f, ioStreams, srcURI, dst, opts)
	case dirCopy:
		srcURI, err := ParseS3URI(src)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		dstURI, err := ParseS3URI(dst)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runCopyMv(ctx, cmd, f, ioStreams, srcURI, dstURI, opts)
	}
	return nil
}

// ----- Upload-move --------------------------------------------------------

func runUploadMv(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src string, dst URI, opts *cpOptions) error {
	info, err := os.Stat(src)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	if info.IsDir() && !opts.Recursive {
		return cmdutil.UsageErrorf(cmd, "source is a directory; pass --recursive to move its contents")
	}
	if !info.IsDir() && opts.Recursive {
		return cmdutil.UsageErrorf(cmd, "--recursive requires the source to be a directory")
	}

	transporter, err := transporterBuilder(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	payload := newCpPayload(opts.Dryrun)
	started := time.Now()

	if opts.Recursive {
		if err := uploadMoveTree(ctx, f, ioStreams, transporter, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		key := singleTargetKey(dst.Key, filepath.Base(src))
		if err := uploadMoveOne(ctx, f, ioStreams, transporter, src, URI{Bucket: dst.Bucket, Key: key}, filepath.Base(src), opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

// uploadMoveTree walks srcDir and moves every matching file. After each
// successful upload the local source is removed; failed transfers leave the
// source untouched.
func uploadMoveTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, tr Transporter, srcDir string, dst URI, opts *cpOptions, payload *cpPayload) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !matchFilters(rel, opts.Include, opts.Exclude) {
			return nil
		}
		key := joinKey(dst.Key, rel)
		return uploadMoveOne(ctx, f, ioStreams, tr, path, URI{Bucket: dst.Bucket, Key: key}, rel, opts, payload)
	})
}

// uploadMoveOne performs a single-file upload-move: upload via uploadOne,
// then remove the local source on success. Dryruns print a "would move"
// preview and touch no disk state.
func uploadMoveOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, tr Transporter, localPath string, dst URI, rel string, opts *cpOptions, payload *cpPayload) error {
	dstStr := dst.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would move %s -> %s\n", rel, dstStr)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      localPath,
			Destination: dstStr,
			Status:      "dryrun",
		})
		return nil
	}

	// Use a throwaway payload for uploadOne so we can replace its line with
	// a "moved" line after the source delete succeeds — and so we can keep
	// the bytes/duration for the real payload. Redirect Out to discard so
	// uploadOne's own "uploaded" human line doesn't appear; we want the
	// human-facing verb to be "moved".
	sub := newCpPayload(false)
	quietStreams := cmdutil.IOStreams{In: ioStreams.In, Out: discardWriter{}, ErrOut: ioStreams.ErrOut}
	if err := uploadOne(ctx, f, quietStreams, tr, localPath, dst, rel, opts, &sub); err != nil {
		return err
	}
	// Remove the local source now that the upload succeeded.
	if err := os.Remove(localPath); err != nil {
		return fmt.Errorf("remove source after upload: %w", err)
	}

	entry := sub.Transfers[len(sub.Transfers)-1]
	payload.Transfers = append(payload.Transfers, entry)
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 moved %s -> %s (%s)\n", localPath, dstStr, humanBytes(entry.Bytes))
	}
	return nil
}

// ----- Download-move ------------------------------------------------------

func runDownloadMv(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src URI, dst string, opts *cpOptions) error {
	if !opts.Recursive && src.Key == "" {
		return cmdutil.UsageErrorf(cmd, "source is a bucket/prefix; pass --recursive to move its contents")
	}

	transporter, err := transporterBuilder(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}
	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	payload := newCpPayload(opts.Dryrun)
	started := time.Now()

	if opts.Recursive {
		if err := downloadMoveTree(ctx, f, ioStreams, apiClient, transporter, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		localPath := resolveDownloadPath(dst, src.Key)
		if err := downloadMoveOne(ctx, f, ioStreams, apiClient, transporter, src, localPath, src.Key, opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

func downloadMoveTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, tr Transporter, src URI, dstDir string, opts *cpOptions, payload *cpPayload) error {
	keys, err := listAllKeys(ctx, f, ioStreams, client, src)
	if err != nil {
		return err
	}
	for _, k := range keys {
		rel := relKey(src.Key, k)
		if !matchFilters(rel, opts.Include, opts.Exclude) {
			continue
		}
		localPath, err := safeJoin(dstDir, rel)
		if err != nil {
			return err
		}
		if err := downloadMoveOne(ctx, f, ioStreams, client, tr, URI{Bucket: src.Bucket, Key: k}, localPath, rel, opts, payload); err != nil {
			return err
		}
	}
	return nil
}

// downloadMoveOne performs a single download-move: download via downloadOne,
// then issue DeleteObject on the S3 source.
func downloadMoveOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, tr Transporter, src URI, localPath, rel string, opts *cpOptions, payload *cpPayload) error {
	srcStr := src.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would move %s -> %s\n", srcStr, localPath)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      srcStr,
			Destination: localPath,
			Status:      "dryrun",
		})
		return nil
	}

	sub := newCpPayload(false)
	quietStreams := cmdutil.IOStreams{In: ioStreams.In, Out: discardWriter{}, ErrOut: ioStreams.ErrOut}
	if err := downloadOne(ctx, f, quietStreams, tr, src, localPath, rel, opts, &sub); err != nil {
		return err
	}

	in := &s3.DeleteObjectInput{Bucket: aws.String(src.Bucket), Key: aws.String(src.Key)}
	out, err := client.DeleteObject(ctx, in)
	if err != nil {
		return translateError(err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "DeleteObject response:", out)

	entry := sub.Transfers[len(sub.Transfers)-1]
	payload.Transfers = append(payload.Transfers, entry)
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 moved %s -> %s (%s)\n", srcStr, localPath, humanBytes(entry.Bytes))
	}
	return nil
}

// ----- S3-to-S3 move ------------------------------------------------------

func runCopyMv(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src, dst URI, opts *cpOptions) error {
	if !opts.Recursive && src.Key == "" {
		return cmdutil.UsageErrorf(cmd, "source is a bucket/prefix; pass --recursive to move its contents")
	}

	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	payload := newCpPayload(opts.Dryrun)
	started := time.Now()

	if opts.Recursive {
		if err := s3MoveTree(ctx, f, ioStreams, apiClient, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		if err := s3MoveOne(ctx, f, ioStreams, apiClient, src, dst, src.Key, opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

func s3MoveTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, src, dst URI, opts *cpOptions, payload *cpPayload) error {
	keys, err := listAllKeys(ctx, f, ioStreams, client, src)
	if err != nil {
		return err
	}
	for _, k := range keys {
		rel := relKey(src.Key, k)
		if !matchFilters(rel, opts.Include, opts.Exclude) {
			continue
		}
		dstKey := joinKey(dst.Key, rel)
		if err := s3MoveOne(ctx, f, ioStreams, client, URI{Bucket: src.Bucket, Key: k}, URI{Bucket: dst.Bucket, Key: dstKey}, rel, opts, payload); err != nil {
			return err
		}
	}
	return nil
}

// s3MoveOne performs a single S3→S3 move: CopyObject then DeleteObject on
// the source. CopyObject's URL-encoding of CopySource is handled by copyOne
// (reused from cp.go).
func s3MoveOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, src, dst URI, rel string, opts *cpOptions, payload *cpPayload) error {
	srcStr := src.String()
	dstStr := dst.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would move %s -> %s\n", srcStr, dstStr)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      srcStr,
			Destination: dstStr,
			Status:      "dryrun",
		})
		return nil
	}

	sub := newCpPayload(false)
	quietStreams := cmdutil.IOStreams{In: ioStreams.In, Out: discardWriter{}, ErrOut: ioStreams.ErrOut}
	if err := copyOne(ctx, f, quietStreams, client, src, dst, rel, opts, &sub); err != nil {
		return err
	}

	in := &s3.DeleteObjectInput{Bucket: aws.String(src.Bucket), Key: aws.String(src.Key)}
	out, err := client.DeleteObject(ctx, in)
	if err != nil {
		return translateError(err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "DeleteObject response:", out)

	entry := sub.Transfers[len(sub.Transfers)-1]
	payload.Transfers = append(payload.Transfers, entry)
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 moved %s -> %s\n", srcStr, dstStr)
	}
	return nil
}

// ----- helpers ------------------------------------------------------------

// discardWriter swallows writes; used to silence the per-file human line
// produced by the inner cp helpers when wrapping them for mv, so mv can
// emit a single "moved" line per entry instead of "uploaded"/"downloaded".
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
