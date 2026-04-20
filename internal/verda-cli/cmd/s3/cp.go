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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// direction enumerates the four possible cp source/destination combinations.
type direction int

const (
	dirInvalid direction = iota
	dirUpload
	dirDownload
	dirCopy
)

// detectDirection returns the direction implied by src/dst. Both-local is
// reported as dirInvalid; the caller turns that into a UsageErrorf.
func detectDirection(src, dst string) direction {
	srcS3 := IsS3URI(src)
	dstS3 := IsS3URI(dst)
	switch {
	case !srcS3 && dstS3:
		return dirUpload
	case srcS3 && !dstS3:
		return dirDownload
	case srcS3 && dstS3:
		return dirCopy
	default:
		return dirInvalid
	}
}

// cpOptions captures flags for the cp command.
type cpOptions struct {
	Recursive   bool
	Include     []string
	Exclude     []string
	Dryrun      bool
	ContentType string
}

// transferEntry is the structured shape for a single completed (or previewed)
// transfer.
type transferEntry struct {
	Source      string `json:"source"            yaml:"source"`
	Destination string `json:"destination"       yaml:"destination"`
	Bytes       int64  `json:"bytes"             yaml:"bytes"`
	DurationMs  int64  `json:"duration_ms"       yaml:"duration_ms"`
	Status      string `json:"status"            yaml:"status"`
}

// cpSummary is the aggregate footer included in structured output.
type cpSummary struct {
	Files      int   `json:"files"       yaml:"files"`
	Bytes      int64 `json:"bytes"       yaml:"bytes"`
	DurationMs int64 `json:"duration_ms" yaml:"duration_ms"`
}

// cpPayload is the JSON/YAML shape emitted by the cp command.
type cpPayload struct {
	Transfers []transferEntry `json:"transfers" yaml:"transfers"`
	Summary   cpSummary       `json:"summary"   yaml:"summary"`
	Dryrun    bool            `json:"dryrun"    yaml:"dryrun"`
}

// NewCmdCp builds the `verda s3 cp` cobra command.
func NewCmdCp(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &cpOptions{}

	cmd := &cobra.Command{
		Use:   "cp <src> <dst>",
		Short: "Copy files between local and S3, or between S3 buckets",
		Long: cmdutil.LongDesc(`
			Copy files between local paths and S3 URIs, or between two S3
			URIs. At least one of <src> or <dst> must be an s3:// URI.

			With --recursive, uploads walk the local directory, and S3
			downloads / copies paginate through every object under the
			source prefix. --include and --exclude glob patterns filter
			the set (matched against the relative path; '*' does not
			cross '/').

			With --dryrun, the planned transfers are listed but no SDK
			calls are made.
		`),
		Example: cmdutil.Examples(`
			# Upload a single file
			verda s3 cp ./report.csv s3://my-bucket/reports/report.csv

			# Download a single object
			verda s3 cp s3://my-bucket/report.csv ./report.csv

			# Copy between buckets
			verda s3 cp s3://src/a.txt s3://dst/b.txt

			# Recursive upload with filter
			verda s3 cp ./data s3://my-bucket/data/ --recursive --include "*.csv"

			# Preview a recursive download
			verda s3 cp s3://my-bucket/logs/ ./logs --recursive --dryrun
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCp(cmd, f, ioStreams, opts, args[0], args[1])
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.Recursive, "recursive", false, "Copy every file under a directory/prefix")
	flags.StringArrayVar(&opts.Include, "include", nil, "Only copy entries matching this glob (repeatable)")
	flags.StringArrayVar(&opts.Exclude, "exclude", nil, "Skip entries matching this glob (repeatable, overrides --include)")
	flags.BoolVar(&opts.Dryrun, "dryrun", false, "Preview transfers without performing them")
	flags.StringVar(&opts.ContentType, "content-type", "", "Override Content-Type on uploads")

	return cmd
}

func runCp(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *cpOptions, src, dst string) error {
	dir := detectDirection(src, dst)
	if dir == dirInvalid {
		return cmdutil.UsageErrorf(cmd, "cp requires at least one s3:// URI")
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	switch dir {
	case dirUpload:
		dstURI, err := ParseS3URI(dst)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runUpload(ctx, cmd, f, ioStreams, src, dstURI, opts)
	case dirDownload:
		srcURI, err := ParseS3URI(src)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runDownload(ctx, cmd, f, ioStreams, srcURI, dst, opts)
	case dirCopy:
		srcURI, err := ParseS3URI(src)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		dstURI, err := ParseS3URI(dst)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "%v", err)
		}
		return runCopy(ctx, cmd, f, ioStreams, srcURI, dstURI, opts)
	}
	return nil
}

// ----- Upload -------------------------------------------------------------

func runUpload(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src string, dst URI, opts *cpOptions) error {
	info, err := os.Stat(src)
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}
	if info.IsDir() && !opts.Recursive {
		return cmdutil.UsageErrorf(cmd, "source is a directory; pass --recursive to upload its contents")
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
		if err := uploadTree(ctx, f, ioStreams, transporter, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		key := singleTargetKey(dst.Key, filepath.Base(src))
		if err := uploadOne(ctx, f, ioStreams, transporter, src, URI{Bucket: dst.Bucket, Key: key}, filepath.Base(src), opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

// uploadTree walks srcDir and uploads every regular file matching the filters.
func uploadTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, tr Transporter, srcDir string, dst URI, opts *cpOptions, payload *cpPayload) error {
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
		return uploadOne(ctx, f, ioStreams, tr, path, URI{Bucket: dst.Bucket, Key: key}, rel, opts, payload)
	})
}

// uploadOne uploads a single local file (or records a dryrun entry).
func uploadOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, tr Transporter, localPath string, dst URI, rel string, opts *cpOptions, payload *cpPayload) error {
	dstStr := dst.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would upload %s -> %s\n", rel, dstStr)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      localPath,
			Destination: dstStr,
			Status:      "dryrun",
		})
		return nil
	}

	file, err := os.Open(localPath) //nolint:gosec // user-supplied path is intentional
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	ct := inferContentType(localPath, opts.ContentType)
	in := &s3.PutObjectInput{
		Bucket:      aws.String(dst.Bucket),
		Key:         aws.String(dst.Key),
		Body:        file,
		ContentType: aws.String(ct),
	}

	started := time.Now()
	_, err = tr.Upload(ctx, in)
	elapsed := time.Since(started)
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Upload response:", struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
		Bytes  int64  `json:"bytes"`
	}{Bucket: dst.Bucket, Key: dst.Key, Bytes: info.Size()})

	payload.Transfers = append(payload.Transfers, transferEntry{
		Source:      localPath,
		Destination: dstStr,
		Bytes:       info.Size(),
		DurationMs:  elapsed.Milliseconds(),
		Status:      "ok",
	})
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 uploaded %s (%s)\n", rel, humanBytes(info.Size()))
	}
	return nil
}

// ----- Download -----------------------------------------------------------

func runDownload(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src URI, dst string, opts *cpOptions) error {
	if !opts.Recursive && src.Key == "" {
		return cmdutil.UsageErrorf(cmd, "source is a bucket/prefix; pass --recursive to download its contents")
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
		if err := downloadTree(ctx, f, ioStreams, apiClient, transporter, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		localPath := resolveDownloadPath(dst, src.Key)
		if err := downloadOne(ctx, f, ioStreams, transporter, src, localPath, src.Key, opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

// downloadTree lists src.Key and downloads each matching object into dstDir
// preserving the relative path below src.Key. Each resolved local path is
// verified to stay within dstDir to block adversarial keys with ".."
// segments.
func downloadTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, tr Transporter, src URI, dstDir string, opts *cpOptions, payload *cpPayload) error {
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
		if err := downloadOne(ctx, f, ioStreams, tr, URI{Bucket: src.Bucket, Key: k}, localPath, rel, opts, payload); err != nil {
			return err
		}
	}
	return nil
}

// downloadOne performs a single GetObject download (or records a dryrun entry).
func downloadOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, tr Transporter, src URI, localPath, rel string, opts *cpOptions, payload *cpPayload) error {
	srcStr := src.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would download %s -> %s\n", srcStr, localPath)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      srcStr,
			Destination: localPath,
			Status:      "dryrun",
		})
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o750); err != nil {
		return err
	}

	file, err := os.Create(localPath) //nolint:gosec // user-supplied path is intentional
	if err != nil {
		return err
	}

	in := &s3.GetObjectInput{Bucket: aws.String(src.Bucket), Key: aws.String(src.Key)}
	started := time.Now()
	n, err := tr.Download(ctx, file, in)
	elapsed := time.Since(started)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(localPath)
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Download response:", struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
		Bytes  int64  `json:"bytes"`
	}{Bucket: src.Bucket, Key: src.Key, Bytes: n})

	payload.Transfers = append(payload.Transfers, transferEntry{
		Source:      srcStr,
		Destination: localPath,
		Bytes:       n,
		DurationMs:  elapsed.Milliseconds(),
		Status:      "ok",
	})
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 downloaded %s (%s)\n", rel, humanBytes(n))
	}
	return nil
}

// ----- S3-to-S3 copy ------------------------------------------------------

func runCopy(ctx context.Context, cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, src, dst URI, opts *cpOptions) error {
	if !opts.Recursive && src.Key == "" {
		return cmdutil.UsageErrorf(cmd, "source is a bucket/prefix; pass --recursive to copy its contents")
	}

	apiClient, err := buildClient(ctx, f, ClientOverrides{})
	if err != nil {
		return err
	}

	payload := newCpPayload(opts.Dryrun)
	started := time.Now()

	if opts.Recursive {
		if err := copyTree(ctx, f, ioStreams, apiClient, src, dst, opts, &payload); err != nil {
			return err
		}
	} else {
		if err := copyOne(ctx, f, ioStreams, apiClient, src, dst, src.Key, opts, &payload); err != nil {
			return err
		}
	}

	return finalizeCp(ioStreams, f, &payload, started, opts.Dryrun)
}

// copyTree paginates src and issues a CopyObject for each matching key.
func copyTree(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, src, dst URI, opts *cpOptions, payload *cpPayload) error {
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
		if err := copyOne(ctx, f, ioStreams, client, URI{Bucket: src.Bucket, Key: k}, URI{Bucket: dst.Bucket, Key: dstKey}, rel, opts, payload); err != nil {
			return err
		}
	}
	return nil
}

// copyOne issues a CopyObject call (or records a dryrun entry).
func copyOne(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, src, dst URI, rel string, opts *cpOptions, payload *cpPayload) error {
	srcStr := src.String()
	dstStr := dst.String()
	structured := isStructured(f.OutputFormat())
	if opts.Dryrun {
		if !structured {
			_, _ = fmt.Fprintf(ioStreams.Out, "(dry run) would copy %s -> %s\n", srcStr, dstStr)
		}
		payload.Transfers = append(payload.Transfers, transferEntry{
			Source:      srcStr,
			Destination: dstStr,
			Status:      "dryrun",
		})
		return nil
	}

	// S3 requires a literal '/' between bucket and key in CopySource; each
	// component must be URL-encoded separately so special characters in the
	// key (spaces, '+', etc.) round-trip correctly.
	copySource := url.PathEscape(src.Bucket) + "/" + url.PathEscape(src.Key)
	in := &s3.CopyObjectInput{
		Bucket:     aws.String(dst.Bucket),
		Key:        aws.String(dst.Key),
		CopySource: aws.String(copySource),
	}
	started := time.Now()
	out, err := client.CopyObject(ctx, in)
	elapsed := time.Since(started)
	if err != nil {
		return translateError(err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "CopyObject response:", out)

	payload.Transfers = append(payload.Transfers, transferEntry{
		Source:      srcStr,
		Destination: dstStr,
		DurationMs:  elapsed.Milliseconds(),
		Status:      "ok",
	})
	if !structured {
		_, _ = fmt.Fprintf(ioStreams.Out, "\u2713 copied %s -> %s\n", srcStr, dstStr)
	}
	return nil
}

// ----- shared helpers -----------------------------------------------------

// isStructured reports whether the output format is a machine-readable one
// that must not be interleaved with human progress lines. "table" (or an
// empty default) yields false.
func isStructured(format string) bool {
	return format == "json" || format == "yaml"
}

func newCpPayload(dryrun bool) cpPayload {
	return cpPayload{
		Transfers: []transferEntry{},
		Dryrun:    dryrun,
	}
}

// finalizeCp emits structured output (if requested) and the human-readable
// footer summarizing the batch.
func finalizeCp(ioStreams cmdutil.IOStreams, f cmdutil.Factory, payload *cpPayload, started time.Time, dryrun bool) error {
	var bytesTotal int64
	for _, t := range payload.Transfers {
		bytesTotal += t.Bytes
	}
	elapsed := time.Since(started)
	payload.Summary = cpSummary{
		Files:      len(payload.Transfers),
		Bytes:      bytesTotal,
		DurationMs: elapsed.Milliseconds(),
	}

	if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), payload); wrote {
		return werr
	}

	if dryrun {
		_, _ = fmt.Fprintf(ioStreams.Out, "%d file(s) planned\n", len(payload.Transfers))
		return nil
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "%d file(s) * %s * %s\n",
		len(payload.Transfers), humanBytes(bytesTotal), elapsed.Truncate(time.Millisecond))
	return nil
}

// listAllKeys paginates ListObjectsV2 under uri and returns every key found.
func listAllKeys(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client API, uri URI) ([]string, error) {
	var (
		keys  []string
		token *string
	)
	for {
		in := &s3.ListObjectsV2Input{Bucket: aws.String(uri.Bucket)}
		if uri.Key != "" {
			in.Prefix = aws.String(uri.Key)
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
			keys = append(keys, aws.ToString(out.Contents[i].Key))
		}
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil || *out.NextContinuationToken == "" {
			break
		}
		token = out.NextContinuationToken
	}
	return keys, nil
}

// joinKey concatenates a destination prefix with a relative path using S3's
// forward-slash separator.
func joinKey(prefix, rel string) string {
	rel = filepath.ToSlash(rel)
	if prefix == "" {
		return rel
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + rel
	}
	return prefix + "/" + rel
}

// relKey returns the portion of key that follows prefix (with a leading '/'
// trimmed). When prefix is empty it simply returns key.
func relKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	if strings.HasSuffix(prefix, "/") {
		return strings.TrimPrefix(key, prefix)
	}
	// Prefix without trailing slash: strip prefix + any leading slash.
	trimmed := strings.TrimPrefix(key, prefix)
	return strings.TrimPrefix(trimmed, "/")
}

// singleTargetKey returns dstKey when dstKey is a concrete object name, or
// joins dstKey + baseName when dstKey looks like a prefix (empty or trailing
// slash).
func singleTargetKey(dstKey, baseName string) string {
	if dstKey == "" || strings.HasSuffix(dstKey, "/") {
		return dstKey + baseName
	}
	return dstKey
}

// resolveDownloadPath returns a concrete local file path. If dst is an
// existing directory, or ends in a separator, the file is placed inside it
// using the basename of the S3 key.
func resolveDownloadPath(dst, srcKey string) string {
	if strings.HasSuffix(dst, string(os.PathSeparator)) || strings.HasSuffix(dst, "/") {
		return filepath.Join(dst, path.Base(srcKey))
	}
	if info, err := os.Stat(dst); err == nil && info.IsDir() {
		return filepath.Join(dst, path.Base(srcKey))
	}
	return dst
}
