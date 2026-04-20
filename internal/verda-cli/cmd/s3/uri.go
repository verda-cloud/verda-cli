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
	"fmt"
	"net/url"
	"strings"
)

// URI represents a parsed s3://bucket/key URI.
type URI struct {
	Bucket string
	Key    string
}

// String renders the canonical s3:// form.
func (u URI) String() string {
	if u.Key == "" {
		return "s3://" + u.Bucket
	}
	return "s3://" + u.Bucket + "/" + u.Key
}

// IsS3URI returns true if s starts with the s3:// scheme.
func IsS3URI(s string) bool {
	return strings.HasPrefix(s, "s3://")
}

// ParseS3URI parses s3://bucket[/key] into a URI.
func ParseS3URI(s string) (URI, error) {
	if s == "" {
		return URI{}, errors.New("empty S3 URI")
	}
	if !IsS3URI(s) {
		return URI{}, fmt.Errorf("not an s3:// URI: %q", s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return URI{}, fmt.Errorf("invalid S3 URI %q: %w", s, err)
	}
	if u.Host == "" {
		return URI{}, fmt.Errorf("missing bucket in S3 URI %q", s)
	}

	key := strings.TrimPrefix(u.Path, "/")
	return URI{Bucket: u.Host, Key: key}, nil
}
