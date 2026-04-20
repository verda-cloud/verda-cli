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
	"errors"
	"net"
	"strings"

	"github.com/aws/smithy-go"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// translateError normalises AWS SDK errors into cmdutil.AgentError values so
// the project-wide error classifier can present them consistently.
func translateError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket":
			return cmdutil.NewNotFoundError("bucket", apiErr.ErrorMessage())
		case "NoSuchKey":
			return cmdutil.NewNotFoundError("object", apiErr.ErrorMessage())
		case "AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch":
			return cmdutil.NewAuthError(apiErr.ErrorMessage())
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return cmdutil.NewValidationError("bucket", apiErr.ErrorMessage())
		}
	}

	if isNetworkError(err) {
		return cmdutil.NewAPIError("cannot reach Verda S3 endpoint: "+err.Error(), 0)
	}

	return err
}

func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout")
}
