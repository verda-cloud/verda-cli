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
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Transporter is the minimal upload/download surface used by cp/mv/sync.
// It abstracts over the aws-sdk-go-v2 transfer manager so tests can inject
// fakes without depending on SDK internals.
type Transporter interface {
	Upload(ctx context.Context, in *s3.PutObjectInput) (*manager.UploadOutput, error)
	Download(ctx context.Context, w io.WriterAt, in *s3.GetObjectInput) (int64, error)
}

// transporterBuilder is a package-level var so tests can swap in a fake
// Transporter. Production code always calls the default via this indirection.
var transporterBuilder = defaultTransporterBuilder

func defaultTransporterBuilder(ctx context.Context, f cmdutil.Factory, ov ClientOverrides) (Transporter, error) {
	creds, err := loadCredsFromFactory(f)
	if err != nil {
		return nil, err
	}

	sdkClient, err := NewClient(ctx, creds, creds.AuthMode, ov)
	if err != nil {
		return nil, err
	}

	//nolint:staticcheck // feature/s3/manager is deprecated in favor of transfermanager,
	// but transfermanager is not yet part of any tagged module release. Switch when available.
	return &sdkTransporter{
		// The Uploader keeps its OWN RequestChecksumCalculation (defaulting to
		// WhenSupported) independent of the s3 client's. Left at the default it
		// adds a CRC32 trailer (aws-chunked / STREAMING-UNSIGNED-PAYLOAD-TRAILER)
		// to every UploadPart, which Ceph RGW rejects with 400
		// XAmzContentSHA256Mismatch. Force WhenRequired to match NewClient.
		up: manager.NewUploader(sdkClient, func(u *manager.Uploader) {
			u.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		}),
		down: manager.NewDownloader(sdkClient),
	}, nil
}

// sdkTransporter wraps the aws-sdk-go-v2 Uploader/Downloader as a Transporter.
type sdkTransporter struct {
	//nolint:staticcheck // see defaultTransporterBuilder.
	up *manager.Uploader
	//nolint:staticcheck // see defaultTransporterBuilder.
	down *manager.Downloader
}

func (t *sdkTransporter) Upload(ctx context.Context, in *s3.PutObjectInput) (*manager.UploadOutput, error) {
	return t.up.Upload(ctx, in) //nolint:staticcheck // see defaultTransporterBuilder.
}

func (t *sdkTransporter) Download(ctx context.Context, w io.WriterAt, in *s3.GetObjectInput) (int64, error) {
	return t.down.Download(ctx, w, in) //nolint:staticcheck // see defaultTransporterBuilder.
}

// safeJoin joins root with the slash-separated rel path, ensuring the result
// stays within root. If rel contains ".." segments that would escape root,
// it returns an error — guards against adversarial S3 keys in recursive
// downloads.
func safeJoin(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}
	candidate := filepath.Join(absRoot, filepath.FromSlash(rel))
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve local path: %w", err)
	}
	if absCandidate != absRoot && !strings.HasPrefix(absCandidate, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("key %q would escape destination directory", rel)
	}
	return absCandidate, nil
}

// inferContentType returns override when non-empty, otherwise
// mime.TypeByExtension(ext), falling back to application/octet-stream.
func inferContentType(path, override string) string {
	if override != "" {
		return override
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// byteUnits maps a size suffix to its multiplier. Both binary (MiB) and the
// loose decimal-looking forms (MB, M) are accepted and treated as binary, since
// part sizes are inherently power-of-two oriented and users rarely mean exactly
// 10^6 here.
var byteUnits = []struct {
	suffix string
	mult   int64
}{
	{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
	{"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
	{"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
	{"B", 1},
}

// parseByteSize parses a human size like "32MiB", "8M", or "1073741824" into
// bytes. An empty string returns 0 (caller treats 0 as "auto"). Suffixes are
// case-insensitive.
func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	upper := strings.ToUpper(s)
	for i := range byteUnits {
		u := strings.ToUpper(byteUnits[i].suffix)
		if strings.HasSuffix(upper, u) {
			num := strings.TrimSpace(upper[:len(upper)-len(u)])
			v, err := strconv.ParseInt(num, 10, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("invalid size %q", s)
			}
			return v * byteUnits[i].mult, nil
		}
	}
	v, err := strconv.ParseInt(upper, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return v, nil
}

// parseOlderThan parses a coarse age like "7d", "12h", "30m" into a Duration.
// It extends time.ParseDuration with a "d" (days) unit, which the stdlib does
// not support. An empty string returns 0 (caller treats 0 as "no age filter").
func parseOlderThan(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(s, "d"), 64)
		if err != nil || days < 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return d, nil
}
