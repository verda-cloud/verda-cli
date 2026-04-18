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
	"io"
	"mime"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
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
	profile := f.Options().AuthOptions.Profile
	path, err := resolveCredentialsFile("")
	if err != nil {
		return nil, err
	}

	creds, err := options.LoadS3CredentialsForProfile(path, profile)
	if err != nil {
		// Missing file / missing profile: still try with an empty credentials struct
		// so NewClient's friendly error fires.
		creds = &options.S3Credentials{}
	}

	sdkClient, err := NewClient(ctx, creds, creds.AuthMode, ov)
	if err != nil {
		return nil, err
	}

	//nolint:staticcheck // feature/s3/manager is deprecated in favor of transfermanager,
	// but transfermanager is not yet part of any tagged module release. Switch when available.
	return &sdkTransporter{
		up:   manager.NewUploader(sdkClient),
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
